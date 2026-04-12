package ddns

import (
	"net"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/lease"
)

type Config struct {
	ServerAddr    string
	ForwardZone   string
	ReverseZone   string
	TSIGKey       string
	TSIGSecret    string
	TSIGAlgorithm string
	Timeout       time.Duration
}

type Action string

const (
	ActionUpsert Action = "upsert"
	ActionDelete Action = "delete"
)

type Request struct {
	Action Action
	Lease  lease.Lease
}

type rr struct {
	name  string
	typ   uint16
	class uint16
	ttl   uint32
	rdata []byte
}

type zoneUpdate struct {
	zone    string
	records []rr
}

func normalizeZone(zone string) string {
	return strings.Trim(strings.TrimSpace(zone), ".")
}

func ipType(ip net.IP) uint16 {
	if ip.To4() != nil {
		return 1
	}
	return 28
}
