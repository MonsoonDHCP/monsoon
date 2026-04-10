package storage

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	defaultSegmentSize = 64 * 1024 * 1024
)

type WAL struct {
	dir         string
	segmentSize int64
	mu          sync.Mutex
	file        *os.File
	offset      int64
	segmentID   int
}

func OpenWAL(dir string) (*WAL, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create wal dir: %w", err)
	}

	w := &WAL{dir: dir, segmentSize: defaultSegmentSize}
	if err := w.openLatest(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *WAL) openLatest() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return fmt.Errorf("read wal dir: %w", err)
	}
	ids := make([]int, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wal") {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(e.Name(), "%09d.wal", &id); err == nil {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	if len(ids) == 0 {
		w.segmentID = 1
		return w.openSegment(w.segmentID)
	}
	w.segmentID = ids[len(ids)-1]
	if err := w.openSegment(w.segmentID); err != nil {
		return err
	}
	st, err := w.file.Stat()
	if err != nil {
		return err
	}
	w.offset = st.Size()
	return nil
}

func (w *WAL) openSegment(id int) error {
	path := filepath.Join(w.dir, fmt.Sprintf("%09d.wal", id))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open wal segment: %w", err)
	}
	w.file = f
	return nil
}

func (w *WAL) rotateIfNeeded(nextEntryBytes int64) error {
	if w.file == nil {
		return errors.New("wal not opened")
	}
	if w.offset+nextEntryBytes <= w.segmentSize {
		return nil
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	w.segmentID++
	w.offset = 0
	return w.openSegment(w.segmentID)
}

func (w *WAL) Append(op OpType, key []byte, value []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	payloadLen := 1 + 2 + len(key) + 4 + len(value)
	buf := make([]byte, 4+4+payloadLen)
	binary.BigEndian.PutUint32(buf[0:4], uint32(payloadLen))
	crc := crc32.ChecksumIEEE(w.marshalPayload(op, key, value))
	binary.BigEndian.PutUint32(buf[4:8], crc)

	payload := buf[8:]
	payload[0] = byte(op)
	binary.BigEndian.PutUint16(payload[1:3], uint16(len(key)))
	copy(payload[3:3+len(key)], key)
	vLenPos := 3 + len(key)
	binary.BigEndian.PutUint32(payload[vLenPos:vLenPos+4], uint32(len(value)))
	copy(payload[vLenPos+4:], value)

	if err := w.rotateIfNeeded(int64(len(buf))); err != nil {
		return err
	}

	n, err := w.file.Write(buf)
	if err != nil {
		return err
	}
	w.offset += int64(n)
	if err := w.file.Sync(); err != nil {
		return err
	}
	return nil
}

func (w *WAL) marshalPayload(op OpType, key []byte, value []byte) []byte {
	payload := make([]byte, 1+2+len(key)+4+len(value))
	payload[0] = byte(op)
	binary.BigEndian.PutUint16(payload[1:3], uint16(len(key)))
	copy(payload[3:3+len(key)], key)
	vLenPos := 3 + len(key)
	binary.BigEndian.PutUint32(payload[vLenPos:vLenPos+4], uint32(len(value)))
	copy(payload[vLenPos+4:], value)
	return payload
}

func (w *WAL) Replay(apply func(op OpType, key []byte, value []byte) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	segments, err := w.segmentPaths()
	if err != nil {
		return err
	}
	for _, p := range segments {
		if err := replayFile(p, apply); err != nil {
			return err
		}
	}
	return nil
}

func (w *WAL) segmentPaths() ([]string, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, err
	}
	type pair struct {
		id   int
		path string
	}
	all := make([]pair, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wal") {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(e.Name(), "%09d.wal", &id); err != nil {
			continue
		}
		all = append(all, pair{id: id, path: filepath.Join(w.dir, e.Name())})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })
	out := make([]string, 0, len(all))
	for _, p := range all {
		out = append(out, p.path)
	}
	return out, nil
}

func replayFile(path string, apply func(op OpType, key []byte, value []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		head := make([]byte, 8)
		_, err := io.ReadFull(r, head)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		length := binary.BigEndian.Uint32(head[0:4])
		expectedCRC := binary.BigEndian.Uint32(head[4:8])
		if length == 0 {
			return nil
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				// truncated tail: ignore partial last entry
				return nil
			}
			return err
		}
		if crc32.ChecksumIEEE(payload) != expectedCRC {
			return fmt.Errorf("wal crc mismatch in %s", path)
		}
		if len(payload) < 7 {
			return fmt.Errorf("invalid wal payload in %s", path)
		}
		op := OpType(payload[0])
		klen := int(binary.BigEndian.Uint16(payload[1:3]))
		if len(payload) < 3+klen+4 {
			return fmt.Errorf("invalid wal key length in %s", path)
		}
		key := append([]byte(nil), payload[3:3+klen]...)
		vlenPos := 3 + klen
		vlen := int(binary.BigEndian.Uint32(payload[vlenPos : vlenPos+4]))
		if len(payload) < vlenPos+4+vlen {
			return fmt.Errorf("invalid wal value length in %s", path)
		}
		value := append([]byte(nil), payload[vlenPos+4:vlenPos+4+vlen]...)
		if err := apply(op, key, value); err != nil {
			return err
		}
	}
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}
