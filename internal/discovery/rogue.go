package discovery

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/dhcpv4"
)

type packetConn interface {
	ReadFrom([]byte) (int, net.Addr, error)
	SetReadDeadline(time.Time) error
	Close() error
}

type packetListenFunc func(network, address string) (packetConn, error)

type RogueDetector struct {
	allowed    map[string]struct{}
	report     func(RogueServer)
	lookupARP  func() map[string]string
	listen     packetListenFunc
	now        func() time.Time
	listenAddr string
}

func NewRogueDetector(allowed []string, report func(RogueServer)) *RogueDetector {
	index := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		if candidate := normalizeIP(item); candidate != "" {
			index[candidate] = struct{}{}
		}
	}
	return &RogueDetector{
		allowed:    index,
		report:     report,
		lookupARP:  readARPNeighbors,
		listen:     defaultPacketListener,
		now:        func() time.Time { return time.Now().UTC() },
		listenAddr: ":68",
	}
}

func defaultPacketListener(network, address string) (packetConn, error) {
	return net.ListenPacket(network, address)
}

func (d *RogueDetector) Start(ctx context.Context) error {
	conn, err := d.listen("udp4", d.listenAddr)
	if err != nil {
		return err
	}
	go d.run(ctx, conn)
	return nil
}

func (d *RogueDetector) run(ctx context.Context, conn packetConn) {
	defer conn.Close()
	buf := make([]byte, 2048)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			return
		}
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		if rogue, ok := d.inspectPacket(buf[:n], addr); ok && d.report != nil {
			d.report(rogue)
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (d *RogueDetector) inspectPacket(raw []byte, addr net.Addr) (RogueServer, bool) {
	packet, err := dhcpv4.DecodePacket(raw)
	if err != nil {
		return RogueServer{}, false
	}
	msgType, ok := packet.Options.MessageType()
	if !ok || msgType != dhcpv4.MessageOffer {
		return RogueServer{}, false
	}

	sourceIP := normalizeIP(ipFromAddr(addr))
	serverID := normalizeIP(serverIdentifier(packet))
	if sourceIP != "" {
		if _, ok := d.allowed[sourceIP]; ok {
			return RogueServer{}, false
		}
	}
	if serverID != "" {
		if _, ok := d.allowed[serverID]; ok {
			return RogueServer{}, false
		}
	}

	suspectIP := serverID
	if suspectIP == "" {
		suspectIP = sourceIP
	}
	if suspectIP == "" {
		return RogueServer{}, false
	}
	arp := map[string]string{}
	if d.lookupARP != nil {
		arp = d.lookupARP()
	}
	mac := strings.ToUpper(strings.TrimSpace(arp[suspectIP]))
	return RogueServer{
		IP:       suspectIP,
		MAC:      mac,
		Vendor:   LookupVendor(mac),
		Source:   rogueSource(sourceIP, serverID),
		Detected: d.now(),
	}, true
}

func rogueSource(sourceIP, serverID string) string {
	switch {
	case sourceIP != "" && serverID != "" && sourceIP != serverID:
		return "dhcp_offer:" + sourceIP + " server_id:" + serverID
	case sourceIP != "":
		return "dhcp_offer:" + sourceIP
	case serverID != "":
		return "server_id:" + serverID
	default:
		return "dhcp_offer"
	}
}

func serverIdentifier(packet dhcpv4.Packet) string {
	if raw, ok := packet.Options[dhcpv4.OptionServerIdentifier]; ok && len(raw) == 4 {
		return net.IP(raw).String()
	}
	if packet.SIAddr != nil {
		return packet.SIAddr.String()
	}
	return ""
}

func ipFromAddr(addr net.Addr) string {
	switch item := addr.(type) {
	case *net.UDPAddr:
		if item.IP != nil {
			return item.IP.String()
		}
	}
	return ""
}

func normalizeIP(value string) string {
	candidate := strings.TrimSpace(value)
	if candidate == "" {
		return ""
	}
	ip := net.ParseIP(candidate)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func encodeRogueServers(items []RogueServer) ([]byte, error) {
	return json.Marshal(items)
}
