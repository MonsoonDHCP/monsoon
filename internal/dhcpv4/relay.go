package dhcpv4

import "strings"

type RelayInfo struct {
	Raw       []byte
	CircuitID string
	RemoteID  string
}

func ParseRelayAgentInfo(raw []byte) RelayInfo {
	info := RelayInfo{Raw: append([]byte(nil), raw...)}
	for i := 0; i+2 <= len(raw); {
		subCode := raw[i]
		subLen := int(raw[i+1])
		i += 2
		if i+subLen > len(raw) {
			break
		}
		data := raw[i : i+subLen]
		i += subLen
		switch subCode {
		case 1:
			info.CircuitID = strings.TrimSpace(string(data))
		case 2:
			info.RemoteID = strings.TrimSpace(string(data))
		}
	}
	return info
}
