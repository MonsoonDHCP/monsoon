package dhcpv4

import "net"

func ResponseTarget(req Packet, remote *net.UDPAddr, resp Packet) *net.UDPAddr {
	if req.GIAddr != nil && !isZeroIPv4(req.GIAddr) {
		return &net.UDPAddr{IP: req.GIAddr.To4(), Port: 67}
	}
	if req.CIAddr != nil && !isZeroIPv4(req.CIAddr) {
		return &net.UDPAddr{IP: req.CIAddr.To4(), Port: 68}
	}
	if resp.YIAddr != nil && !isZeroIPv4(resp.YIAddr) {
		return &net.UDPAddr{IP: resp.YIAddr.To4(), Port: 68}
	}
	return remote
}
