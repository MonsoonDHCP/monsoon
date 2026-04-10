package storage

import (
	"bytes"
	"fmt"
	"sync"
)

type IndexFunc func(primaryKey, value []byte) [][]byte

type IndexManager struct {
	mu      sync.RWMutex
	indexes map[string]IndexFunc
	trees   map[string]*BTree
}

func NewIndexManager() *IndexManager {
	return &IndexManager{
		indexes: make(map[string]IndexFunc),
		trees:   make(map[string]*BTree),
	}
}

func (m *IndexManager) Create(name string, fn IndexFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.indexes[name]; exists {
		return fmt.Errorf("index %s already exists", name)
	}
	m.indexes[name] = fn
	m.trees[name] = NewBTree()
	return nil
}

func (m *IndexManager) Drop(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.indexes, name)
	delete(m.trees, name)
}

func (m *IndexManager) Update(primaryKey, oldValue, newValue []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, fn := range m.indexes {
		tree := m.trees[name]
		if oldValue != nil {
			for _, idxKey := range fn(primaryKey, oldValue) {
				tree.Delete(composeIndexKey(idxKey, primaryKey))
			}
		}
		if newValue != nil {
			for _, idxKey := range fn(primaryKey, newValue) {
				tree.Set(composeIndexKey(idxKey, primaryKey), primaryKey)
			}
		}
	}
}

func (m *IndexManager) Scan(name string, prefix []byte) [][]byte {
	m.mu.RLock()
	tree := m.trees[name]
	m.mu.RUnlock()
	if tree == nil {
		return nil
	}
	res := make([][]byte, 0)
	tree.Iterate(prefix, nil, func(k, v []byte) bool {
		if !bytes.HasPrefix(k, prefix) {
			return false
		}
		res = append(res, append([]byte(nil), v...))
		return true
	})
	return res
}

func composeIndexKey(indexKey, primaryKey []byte) []byte {
	out := make([]byte, 0, len(indexKey)+1+len(primaryKey))
	out = append(out, indexKey...)
	out = append(out, 0)
	out = append(out, primaryKey...)
	return out
}
