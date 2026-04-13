package ddns

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	typeTSIG        = 250
	classINET       = 1
	classNONE       = 254
	classANY        = 255
	typeSOA         = 6
	opcodeUpdate    = 5
	algorithmSHA256 = "hmac-sha256."
)

type tsigSigner struct {
	keyName   string
	algorithm string
	secret    []byte
	fudge     uint16
}

func newTSIGSigner(keyName, secret, algorithm string) (*tsigSigner, error) {
	keyName = strings.TrimSpace(keyName)
	secret = strings.TrimSpace(secret)
	if keyName == "" || secret == "" {
		return nil, nil
	}
	algo := strings.ToLower(strings.Trim(strings.TrimSpace(algorithm), "."))
	if algo == "" {
		algo = "hmac-sha256"
	}
	if algo != "hmac-sha256" {
		return nil, fmt.Errorf("unsupported tsig algorithm %q", algorithm)
	}
	if err := validateDNSName(keyName); err != nil {
		return nil, fmt.Errorf("invalid tsig key name %q: %w", keyName, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("decode tsig secret: %w", err)
	}
	return &tsigSigner{
		keyName:   ensureFQDN(keyName),
		algorithm: algorithmSHA256,
		secret:    decoded,
		fudge:     300,
	}, nil
}

func (s *tsigSigner) sign(message []byte, msgID uint16, now time.Time) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	unix := now.UTC().Unix()
	if unix < 0 {
		return nil, errors.New("tsig timestamp before unix epoch is not supported")
	}
	timeSigned := uint64(unix)
	var macInput []byte
	macInput = append(macInput, message...)
	macInput = append(macInput, encodeNameCanonical(s.keyName)...)
	macInput = appendUint16(macInput, classANY)
	macInput = appendUint32(macInput, 0)
	macInput = append(macInput, encodeNameCanonical(s.algorithm)...)
	macInput = appendTimeSigned(macInput, timeSigned)
	macInput = appendUint16(macInput, s.fudge)
	macInput = appendUint16(macInput, 0)
	macInput = appendUint16(macInput, 0)

	h := hmac.New(sha256.New, s.secret)
	_, _ = h.Write(macInput)
	mac := h.Sum(nil)
	if len(mac) > math.MaxUint16 {
		return nil, fmt.Errorf("tsig mac too long: %d", len(mac))
	}

	rdata := make([]byte, 0, 128)
	rdata = append(rdata, encodeName(s.algorithm)...)
	rdata = appendTimeSigned(rdata, timeSigned)
	rdata = appendUint16(rdata, s.fudge)
	// #nosec G115 -- validated above: len(mac) <= math.MaxUint16.
	rdata = appendUint16(rdata, uint16(len(mac)))
	rdata = append(rdata, mac...)
	rdata = appendUint16(rdata, msgID)
	rdata = appendUint16(rdata, 0)
	rdata = appendUint16(rdata, 0)

	record := make([]byte, 0, 256)
	record = append(record, encodeName(s.keyName)...)
	record = appendUint16(record, typeTSIG)
	record = appendUint16(record, classANY)
	record = appendUint32(record, 0)
	if len(rdata) > math.MaxUint16 {
		return nil, fmt.Errorf("tsig rdata too long: %d", len(rdata))
	}
	// #nosec G115 -- validated above: len(rdata) <= math.MaxUint16.
	record = appendUint16(record, uint16(len(rdata)))
	record = append(record, rdata...)
	return record, nil
}

func appendTimeSigned(dst []byte, value uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], value)
	return append(dst, buf[2:]...)
}
