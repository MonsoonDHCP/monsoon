package dhcpv6

import "time"

func durationToUint32Seconds(value time.Duration) uint32 {
	if value <= 0 {
		return 0
	}
	seconds := value / time.Second
	if seconds <= 0 {
		return 1
	}
	max := time.Duration(^uint32(0))
	if seconds > max {
		return ^uint32(0)
	}
	// #nosec G115 -- bounded to uint32 range above.
	return uint32(seconds)
}
