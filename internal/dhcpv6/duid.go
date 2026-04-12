package dhcpv6

import "encoding/binary"

func GenerateDUIDUUID(uuid [16]byte) []byte {
	buf := make([]byte, 18)
	binary.BigEndian.PutUint16(buf[0:2], 4)
	copy(buf[2:18], uuid[:])
	return buf
}
