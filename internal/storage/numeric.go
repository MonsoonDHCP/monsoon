package storage

import "fmt"

func safeUint16FromInt(value int, field string) (uint16, error) {
	if value < 0 || value > 0xffff {
		return 0, fmt.Errorf("%s out of uint16 range: %d", field, value)
	}
	// #nosec G115 -- bounds checked above.
	return uint16(value), nil
}

func safeUint32FromInt(value int, field string) (uint32, error) {
	if value < 0 || value > 0xffffffff {
		return 0, fmt.Errorf("%s out of uint32 range: %d", field, value)
	}
	// #nosec G115 -- bounds checked above.
	return uint32(value), nil
}
