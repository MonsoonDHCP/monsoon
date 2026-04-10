package storage

import (
	"bytes"
	"sort"
	"sync"
)

type BTree struct {
	mu    sync.RWMutex
	keys  [][]byte
	items map[string][]byte
}

func NewBTree() *BTree {
	return &BTree{
		keys:  make([][]byte, 0, 1024),
		items: make(map[string][]byte),
	}
}

func (t *BTree) Get(key []byte) ([]byte, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	v, ok := t.items[string(key)]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), v...), true
}

func (t *BTree) Set(key []byte, value []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sKey := string(key)
	if _, exists := t.items[sKey]; !exists {
		idx := sort.Search(len(t.keys), func(i int) bool {
			return bytes.Compare(t.keys[i], key) >= 0
		})
		t.keys = append(t.keys, nil)
		copy(t.keys[idx+1:], t.keys[idx:])
		t.keys[idx] = append([]byte(nil), key...)
	}
	t.items[sKey] = append([]byte(nil), value...)
}

func (t *BTree) Delete(key []byte) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	sKey := string(key)
	if _, ok := t.items[sKey]; !ok {
		return false
	}
	delete(t.items, sKey)
	idx := sort.Search(len(t.keys), func(i int) bool {
		return bytes.Compare(t.keys[i], key) >= 0
	})
	if idx < len(t.keys) && bytes.Equal(t.keys[idx], key) {
		copy(t.keys[idx:], t.keys[idx+1:])
		t.keys = t.keys[:len(t.keys)-1]
	}
	return true
}

func (t *BTree) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.items)
}

func (t *BTree) Snapshot() [][2][]byte {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([][2][]byte, 0, len(t.keys))
	for _, key := range t.keys {
		v := t.items[string(key)]
		out = append(out, [2][]byte{append([]byte(nil), key...), append([]byte(nil), v...)})
	}
	return out
}

func (t *BTree) Iterate(start []byte, end []byte, fn func(k, v []byte) bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	idx := 0
	if len(start) > 0 {
		idx = sort.Search(len(t.keys), func(i int) bool {
			return bytes.Compare(t.keys[i], start) >= 0
		})
	}
	for ; idx < len(t.keys); idx++ {
		k := t.keys[idx]
		if len(end) > 0 && bytes.Compare(k, end) > 0 {
			break
		}
		if !fn(append([]byte(nil), k...), append([]byte(nil), t.items[string(k)]...)) {
			return
		}
	}
}
