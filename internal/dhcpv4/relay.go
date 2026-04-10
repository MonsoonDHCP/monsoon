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

func BuildRelayAgentInfo(circuitID, remoteID string) []byte {
	out := make([]byte, 0, len(circuitID)+len(remoteID)+4)
	if circuitID != "" {
		out = append(out, 1, byte(len(circuitID)))
		out = append(out, []byte(circuitID)...)
	}
	if remoteID != "" {
		out = append(out, 2, byte(len(remoteID)))
		out = append(out, []byte(remoteID)...)
	}
	return out
}
