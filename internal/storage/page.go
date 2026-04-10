package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const PageSize = 4096

type PageManager struct {
	mu       sync.Mutex
	file     *os.File
	freeList []uint64
}

func OpenPageManager(dir string) (*PageManager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "pages.dat"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	return &PageManager{file: f, freeList: make([]uint64, 0)}, nil
}

func (pm *PageManager) Allocate() (uint64, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if n := len(pm.freeList); n > 0 {
		id := pm.freeList[n-1]
		pm.freeList = pm.freeList[:n-1]
		return id, nil
	}
	st, err := pm.file.Stat()
	if err != nil {
		return 0, err
	}
	return uint64(st.Size() / PageSize), nil
}

func (pm *PageManager) Free(id uint64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.freeList = append(pm.freeList, id)
}

func (pm *PageManager) Read(id uint64) ([]byte, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	buf := make([]byte, PageSize)
	offset := int64(id) * PageSize
	_, err := pm.file.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

func (pm *PageManager) Write(id uint64, data []byte) error {
	if len(data) > PageSize {
		return fmt.Errorf("page too large: %d", len(data))
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()
	buf := make([]byte, PageSize)
	copy(buf, data)
	offset := int64(id) * PageSize
	if _, err := pm.file.WriteAt(buf, offset); err != nil {
		return err
	}
	return pm.file.Sync()
}

func (pm *PageManager) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.file == nil {
		return nil
	}
	return pm.file.Close()
}
