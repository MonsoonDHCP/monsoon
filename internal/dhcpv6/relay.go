package dhcpv6

func encapsulateRelayReply(req Packet, inner Packet) Packet {
	resp := Packet{
		MessageType: MessageRelayReply,
		HopCount:    req.HopCount,
		LinkAddress: req.LinkAddress,
		PeerAddress: req.PeerAddress,
		Options:     Options{},
	}
	if raw, ok := req.Options.Get(OptionInterfaceID); ok {
		resp.Options.Add(OptionInterfaceID, raw)
	}
	raw, _ := inner.Encode()
	resp.Options.Add(OptionRelayMessage, raw)
	return resp
}
