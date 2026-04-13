package storage

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var snapshotMagic = [8]byte{'M', 'O', 'N', 'S', 'N', 'A', 'P', '1'}

func WriteSnapshot(path string, trees map[string]*BTree) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	// #nosec G304 -- path is controlled by internal storage engine configuration.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)

	if _, err := w.Write(snapshotMagic[:]); err != nil {
		return err
	}
	treeCount, err := safeUint32FromInt(len(trees), "snapshot tree count")
	if err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, treeCount); err != nil {
		return err
	}

	for name, tree := range trees {
		rows := tree.Snapshot()
		nameLen, err := safeUint16FromInt(len(name), "snapshot tree name length")
		if err != nil {
			return err
		}
		if err := binary.Write(w, binary.BigEndian, nameLen); err != nil {
			return err
		}
		if _, err := w.WriteString(name); err != nil {
			return err
		}
		rowCount, err := safeUint32FromInt(len(rows), "snapshot row count")
		if err != nil {
			return err
		}
		if err := binary.Write(w, binary.BigEndian, rowCount); err != nil {
			return err
		}
		for _, row := range rows {
			keyLen, err := safeUint32FromInt(len(row[0]), "snapshot key length")
			if err != nil {
				return err
			}
			if err := binary.Write(w, binary.BigEndian, keyLen); err != nil {
				return err
			}
			if _, err := w.Write(row[0]); err != nil {
				return err
			}
			valueLen, err := safeUint32FromInt(len(row[1]), "snapshot value length")
			if err != nil {
				return err
			}
			if err := binary.Write(w, binary.BigEndian, valueLen); err != nil {
				return err
			}
			if _, err := w.Write(row[1]); err != nil {
				return err
			}
		}
	}

	if err := w.Flush(); err != nil {
		return err
	}
	return f.Sync()
}

func ReadSnapshot(path string) (map[string]*BTree, error) {
	// #nosec G304 -- path is controlled by internal storage engine configuration.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReader(f)

	magic := make([]byte, len(snapshotMagic))
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, err
	}
	if string(magic) != string(snapshotMagic[:]) {
		return nil, fmt.Errorf("invalid snapshot file")
	}

	var treeCount uint32
	if err := binary.Read(r, binary.BigEndian, &treeCount); err != nil {
		return nil, err
	}

	out := make(map[string]*BTree, treeCount)
	for i := uint32(0); i < treeCount; i++ {
		var nameLen uint16
		if err := binary.Read(r, binary.BigEndian, &nameLen); err != nil {
			return nil, err
		}
		nameRaw := make([]byte, nameLen)
		if _, err := io.ReadFull(r, nameRaw); err != nil {
			return nil, err
		}
		name := string(nameRaw)
		var rowCount uint32
		if err := binary.Read(r, binary.BigEndian, &rowCount); err != nil {
			return nil, err
		}
		tree := NewBTree()
		for j := uint32(0); j < rowCount; j++ {
			var keyLen uint32
			if err := binary.Read(r, binary.BigEndian, &keyLen); err != nil {
				return nil, err
			}
			key := make([]byte, keyLen)
			if _, err := io.ReadFull(r, key); err != nil {
				return nil, err
			}
			var valueLen uint32
			if err := binary.Read(r, binary.BigEndian, &valueLen); err != nil {
				return nil, err
			}
			value := make([]byte, valueLen)
			if _, err := io.ReadFull(r, value); err != nil {
				return nil, err
			}
			tree.Set(key, value)
		}
		out[name] = tree
	}

	return out, nil
}
