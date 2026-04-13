package ddns

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/lease"
)

type Client struct {
	serverAddr  string
	forwardZone string
	reverseZone string
	timeout     time.Duration
	signer      *tsigSigner
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.ServerAddr) == "" {
		return nil, errors.New("ddns server address is required")
	}
	addr := strings.TrimSpace(cfg.ServerAddr)
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "53")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	signer, err := newTSIGSigner(cfg.TSIGKey, cfg.TSIGSecret, cfg.TSIGAlgorithm)
	if err != nil {
		return nil, err
	}
	return &Client{
		serverAddr:  addr,
		forwardZone: normalizeZone(cfg.ForwardZone),
		reverseZone: normalizeZone(cfg.ReverseZone),
		timeout:     timeout,
		signer:      signer,
	}, nil
}

func (c *Client) Apply(ctx context.Context, item lease.Lease) error {
	updates, err := c.buildUpdates(ActionUpsert, item)
	if err != nil {
		return err
	}
	for _, update := range updates {
		if err := c.sendUpdate(ctx, update); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, item lease.Lease) error {
	updates, err := c.buildUpdates(ActionDelete, item)
	if err != nil {
		return err
	}
	for _, update := range updates {
		if err := c.sendUpdate(ctx, update); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) buildUpdates(action Action, item lease.Lease) ([]zoneUpdate, error) {
	ip := net.ParseIP(strings.TrimSpace(item.IP))
	if ip == nil {
		return nil, fmt.Errorf("invalid lease ip %q", item.IP)
	}
	host := normalizeHostname(item.Hostname)
	if host == "" {
		return nil, nil
	}
	ttl := defaultTTLLease(item.Duration)
	fqdn := c.forwardName(host)
	if fqdn == "" {
		return nil, nil
	}
	if err := validateDNSName(fqdn); err != nil {
		return nil, err
	}
	if c.forwardZone != "" {
		if err := validateDNSName(c.forwardZone); err != nil {
			return nil, err
		}
	}

	var updates []zoneUpdate
	if c.forwardZone != "" {
		recordType := ipType(ip)
		switch action {
		case ActionUpsert:
			updates = append(updates, zoneUpdate{
				zone: c.forwardZone,
				records: []rr{
					{name: fqdn, typ: recordType, class: classANY, ttl: 0},
					{name: fqdn, typ: recordType, class: classINET, ttl: ttl, rdata: ipRData(ip)},
				},
			})
		case ActionDelete:
			updates = append(updates, zoneUpdate{
				zone: c.forwardZone,
				records: []rr{
					{name: fqdn, typ: recordType, class: classNONE, ttl: 0, rdata: ipRData(ip)},
				},
			})
		}
	}

	ptrName, ptrZone := reverseNames(ip, c.reverseZone)
	if ptrName != "" && ptrZone != "" {
		ptrTarget := ensureFQDN(fqdn)
		if err := validateDNSName(ptrName); err != nil {
			return nil, err
		}
		if err := validateDNSName(ptrZone); err != nil {
			return nil, err
		}
		if err := validateDNSName(ptrTarget); err != nil {
			return nil, err
		}
		switch action {
		case ActionUpsert:
			updates = append(updates, zoneUpdate{
				zone: ptrZone,
				records: []rr{
					{name: ptrName, typ: 12, class: classANY, ttl: 0},
					{name: ptrName, typ: 12, class: classINET, ttl: ttl, rdata: encodeName(ptrTarget)},
				},
			})
		case ActionDelete:
			updates = append(updates, zoneUpdate{
				zone: ptrZone,
				records: []rr{
					{name: ptrName, typ: 12, class: classNONE, ttl: 0, rdata: encodeName(ptrTarget)},
				},
			})
		}
	}
	return updates, nil
}

func (c *Client) sendUpdate(ctx context.Context, update zoneUpdate) error {
	if strings.TrimSpace(update.zone) == "" || len(update.records) == 0 {
		return nil
	}
	if err := validateZoneUpdate(update); err != nil {
		return err
	}
	now := time.Now().UTC()
	msgID, err := nextMessageID()
	if err != nil {
		return fmt.Errorf("generate dns message id: %w", err)
	}
	wire, err := c.buildMessage(msgID, update, now)
	if err != nil {
		return err
	}
	var d net.Dialer
	if deadline, ok := ctx.Deadline(); ok {
		d.Timeout = time.Until(deadline)
	}
	if d.Timeout <= 0 {
		d.Timeout = c.timeout
	}
	conn, err := d.DialContext(ctx, "udp", c.serverAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	if _, err := conn.Write(wire); err != nil {
		return err
	}
	resp := make([]byte, 1500)
	n, err := conn.Read(resp)
	if err != nil {
		return err
	}
	return validateResponse(resp[:n], msgID)
}

func (c *Client) buildMessage(msgID uint16, update zoneUpdate, now time.Time) ([]byte, error) {
	if len(update.records) > math.MaxUint16 {
		return nil, fmt.Errorf("too many dns update records: %d", len(update.records))
	}
	base := make([]byte, 12)
	binary.BigEndian.PutUint16(base[0:2], msgID)
	binary.BigEndian.PutUint16(base[2:4], uint16(opcodeUpdate<<11))
	binary.BigEndian.PutUint16(base[4:6], 1)
	binary.BigEndian.PutUint16(base[6:8], 0)
	// #nosec G115 -- validated above: len(update.records) <= math.MaxUint16.
	binary.BigEndian.PutUint16(base[8:10], uint16(len(update.records)))
	binary.BigEndian.PutUint16(base[10:12], 0)

	body := append([]byte(nil), base...)
	body = append(body, encodeName(update.zone)...)
	body = appendUint16(body, typeSOA)
	body = appendUint16(body, classINET)
	for _, record := range update.records {
		encoded, err := encodeRR(record)
		if err != nil {
			return nil, err
		}
		body = append(body, encoded...)
	}
	if c.signer == nil {
		return body, nil
	}
	tsig, err := c.signer.sign(body, msgID, now)
	if err != nil {
		return nil, err
	}
	final := append([]byte(nil), body...)
	binary.BigEndian.PutUint16(final[10:12], 1)
	final = append(final, tsig...)
	return final, nil
}

func validateResponse(msg []byte, id uint16) error {
	if len(msg) < 12 {
		return errors.New("short dns response")
	}
	if binary.BigEndian.Uint16(msg[0:2]) != id {
		return errors.New("dns response id mismatch")
	}
	flags := binary.BigEndian.Uint16(msg[2:4])
	rcode := flags & 0x000f
	if rcode != 0 {
		return fmt.Errorf("dns update rejected with rcode=%d", rcode)
	}
	return nil
}

func encodeRR(record rr) ([]byte, error) {
	if len(record.rdata) > math.MaxUint16 {
		return nil, fmt.Errorf("record rdata too long for %s", record.name)
	}
	out := make([]byte, 0, 256)
	out = append(out, encodeName(record.name)...)
	out = appendUint16(out, record.typ)
	out = appendUint16(out, record.class)
	out = appendUint32(out, record.ttl)
	// #nosec G115 -- validated above: len(record.rdata) <= math.MaxUint16.
	out = appendUint16(out, uint16(len(record.rdata)))
	out = append(out, record.rdata...)
	return out, nil
}

func encodeName(name string) []byte {
	name = ensureFQDN(name)
	if name == "." {
		return []byte{0}
	}
	trimmed := strings.TrimSuffix(name, ".")
	var out []byte
	for _, label := range strings.Split(trimmed, ".") {
		// #nosec G115 -- label length is constrained by validateDNSName (<= 63).
		out = append(out, byte(len(label)))
		out = append(out, []byte(label)...)
	}
	out = append(out, 0)
	return out
}

func encodeNameCanonical(name string) []byte {
	name = strings.ToLower(ensureFQDN(name))
	return encodeName(name)
}

func appendUint16(dst []byte, value uint16) []byte {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], value)
	return append(dst, buf[:]...)
}

func appendUint32(dst []byte, value uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	return append(dst, buf[:]...)
}

func ensureFQDN(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "."
	}
	name = strings.Trim(name, ".")
	if name == "" {
		return "."
	}
	return name + "."
}

func normalizeHostname(host string) string {
	host = strings.Trim(strings.TrimSpace(host), ".")
	return host
}

func defaultTTLLease(duration time.Duration) uint32 {
	if duration <= 0 {
		return 300
	}
	ttlSeconds := duration / time.Second
	if ttlSeconds == 0 {
		return 300
	}
	if ttlSeconds > time.Duration(math.MaxUint32) {
		return math.MaxUint32
	}
	// #nosec G115 -- ttlSeconds is bounded to uint32 range above.
	return uint32(ttlSeconds)
}

func (c *Client) forwardName(host string) string {
	host = normalizeHostname(host)
	if host == "" {
		return ""
	}
	if strings.Contains(host, ".") {
		return host
	}
	if c.forwardZone == "" {
		return ""
	}
	return host + "." + c.forwardZone
}

func ipRData(ip net.IP) []byte {
	if v4 := ip.To4(); v4 != nil {
		return append([]byte(nil), v4...)
	}
	return append([]byte(nil), ip.To16()...)
}

func reverseNames(ip net.IP, configuredZone string) (name string, zone string) {
	if ip == nil {
		return "", ""
	}
	if v4 := ip.To4(); v4 != nil {
		addr, _ := netip.AddrFromSlice(v4)
		full := reverseIPv4Name(addr)
		return trimZoneName(full, configuredZone)
	}
	addr, ok := netip.AddrFromSlice(ip.To16())
	if !ok {
		return "", ""
	}
	full := reverseIPv6Name(addr)
	return trimZoneName(full, configuredZone)
}

func trimZoneName(full, configuredZone string) (string, string) {
	full = normalizeZone(full)
	configuredZone = normalizeZone(configuredZone)
	if full == "" {
		return "", ""
	}
	if configuredZone == "" {
		parts := strings.Split(full, ".")
		if len(parts) < 2 {
			return full, full
		}
		return strings.Join(parts[:1], "."), strings.Join(parts[1:], ".")
	}
	suffix := "." + configuredZone
	if full == configuredZone {
		return "@", configuredZone
	}
	if strings.HasSuffix(full, suffix) {
		name := strings.TrimSuffix(full, suffix)
		name = strings.TrimSuffix(name, ".")
		if name == "" {
			name = "@"
		}
		return name, configuredZone
	}
	return full, configuredZone
}

func reverseIPv4Name(addr netip.Addr) string {
	ip := addr.As4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", ip[3], ip[2], ip[1], ip[0])
}

func reverseIPv6Name(addr netip.Addr) string {
	ip := addr.As16()
	var labels []string
	for i := len(ip) - 1; i >= 0; i-- {
		labels = append(labels, fmt.Sprintf("%x", ip[i]&0x0f))
		labels = append(labels, fmt.Sprintf("%x", ip[i]>>4))
	}
	return strings.Join(labels, ".") + ".ip6.arpa"
}

func nextMessageID() (uint16, error) {
	var id [2]byte
	if _, err := crand.Read(id[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(id[:]), nil
}

func validateZoneUpdate(update zoneUpdate) error {
	if err := validateDNSName(update.zone); err != nil {
		return fmt.Errorf("invalid update zone %q: %w", update.zone, err)
	}
	for _, record := range update.records {
		if err := validateDNSName(record.name); err != nil {
			return fmt.Errorf("invalid record name %q: %w", record.name, err)
		}
	}
	return nil
}

func validateDNSName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("dns name is empty")
	}
	if name == "@" {
		return nil
	}
	trimmed := strings.TrimSuffix(ensureFQDN(name), ".")
	if len(trimmed) == 0 {
		return nil
	}
	if len(trimmed) > 253 {
		return fmt.Errorf("dns name length %d exceeds max 253", len(trimmed))
	}
	for _, label := range strings.Split(trimmed, ".") {
		if len(label) == 0 {
			return errors.New("dns name contains empty label")
		}
		if len(label) > 63 {
			return fmt.Errorf("dns label %q length %d exceeds max 63", label, len(label))
		}
	}
	return nil
}
