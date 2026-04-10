package storage

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const treeKeySep = '\x1f'

type Engine struct {
	dir    string
	mu     sync.RWMutex
	trees  map[string]*BTree
	index  *IndexManager
	wal    *WAL
	pages  *PageManager
	closed bool
}

func OpenEngine(dataDir string, treeNames []string) (*Engine, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	eng := &Engine{
		dir:   dataDir,
		trees: make(map[string]*BTree),
		index: NewIndexManager(),
	}

	if err := eng.loadSnapshot(); err != nil {
		return nil, err
	}
	for _, n := range treeNames {
		if _, ok := eng.trees[n]; !ok {
			eng.trees[n] = NewBTree()
		}
	}

	pages, err := OpenPageManager(filepath.Join(dataDir, "pages"))
	if err != nil {
		return nil, err
	}
	eng.pages = pages

	wal, err := OpenWAL(filepath.Join(dataDir, "wal"))
	if err != nil {
		return nil, err
	}
	eng.wal = wal

	if err := eng.wal.Replay(func(op OpType, key []byte, value []byte) error {
		treeName, realKey, err := splitTreeKey(key)
		if err != nil {
			return err
		}
		tree := eng.mustTree(treeName)
		switch op {
		case OpPut:
			tree.Set(realKey, value)
		case OpDel:
			tree.Delete(realKey)
		default:
			return fmt.Errorf("unknown wal op %d", op)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return eng, nil
}

func (e *Engine) mustTree(name string) *BTree {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.trees[name]
	if !ok {
		t = NewBTree()
		e.trees[name] = t
	}
	return t
}

func (e *Engine) Get(tree string, key []byte) ([]byte, error) {
	e.mu.RLock()
	t, ok := e.trees[tree]
	e.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	v, ok := t.Get(key)
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (e *Engine) Put(tree string, key []byte, value []byte) error {
	return e.Tx(func(tx *Tx) error {
		tx.Put(tree, key, value)
		return nil
	})
}

func (e *Engine) Delete(tree string, key []byte) error {
	return e.Tx(func(tx *Tx) error {
		tx.Delete(tree, key)
		return nil
	})
}

func (e *Engine) Iterate(tree string, start, end []byte, fn func(key, value []byte) bool) error {
	e.mu.RLock()
	t, ok := e.trees[tree]
	e.mu.RUnlock()
	if !ok {
		return ErrNotFound
	}
	t.Iterate(start, end, fn)
	return nil
}

func (e *Engine) Tx(fn func(tx *Tx) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("engine closed")
	}
	tx := &Tx{mutations: make([]Mutation, 0, 8)}
	if err := fn(tx); err != nil {
		return err
	}

	for _, m := range tx.mutations {
		tree := e.mustTreeUnlocked(m.Tree)
		combined := makeTreeKey(m.Tree, m.Key)
		if err := e.wal.Append(m.Op, combined, m.Value); err != nil {
			return err
		}
		old, _ := tree.Get(m.Key)
		switch m.Op {
		case OpPut:
			tree.Set(m.Key, m.Value)
			e.index.Update(m.Key, old, m.Value)
		case OpDel:
			tree.Delete(m.Key)
			e.index.Update(m.Key, old, nil)
		}
	}
	return nil
}

func (e *Engine) mustTreeUnlocked(name string) *BTree {
	t, ok := e.trees[name]
	if !ok {
		t = NewBTree()
		e.trees[name] = t
	}
	return t
}

func (e *Engine) CreateSnapshot() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	path := filepath.Join(e.dir, "snapshot.bin")
	return WriteSnapshot(path, e.trees)
}

func (e *Engine) loadSnapshot() error {
	path := filepath.Join(e.dir, "snapshot.bin")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	trees, err := ReadSnapshot(path)
	if err != nil {
		return err
	}
	e.trees = trees
	return nil
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	var errs []error
	if e.wal != nil {
		if err := e.wal.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if e.pages != nil {
		if err := e.pages.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errorsJoin(errs...)
	}
	return nil
}

type Tx struct {
	mutations []Mutation
}

func (tx *Tx) Put(tree string, key []byte, value []byte) {
	tx.mutations = append(tx.mutations, Mutation{Tree: tree, Op: OpPut, Key: append([]byte(nil), key...), Value: append([]byte(nil), value...)})
}

func (tx *Tx) Delete(tree string, key []byte) {
	tx.mutations = append(tx.mutations, Mutation{Tree: tree, Op: OpDel, Key: append([]byte(nil), key...)})
}

func makeTreeKey(tree string, key []byte) []byte {
	out := make([]byte, 0, len(tree)+1+len(key))
	out = append(out, []byte(tree)...)
	out = append(out, treeKeySep)
	out = append(out, key...)
	return out
}

func splitTreeKey(key []byte) (tree string, realKey []byte, err error) {
	idx := bytes.IndexByte(key, treeKeySep)
	if idx <= 0 {
		return "", nil, fmt.Errorf("invalid wal tree key")
	}
	return string(key[:idx]), append([]byte(nil), key[idx+1:]...), nil
}

func errorsJoin(errs ...error) error {
	if len(errs) == 0 {
		return nil
	}
	var b bytes.Buffer
	for i, err := range errs {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(err.Error())
	}
	return errors.New(b.String())
}
