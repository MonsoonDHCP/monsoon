package storage

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const treeKeySep = '\x1f'

type Engine struct {
	dir         string
	mu          sync.RWMutex
	trees       map[string]*BTree
	index       *IndexManager
	wal         *WAL
	closed      bool
	sequence    int64
	nextWatchID int64
	watchers    map[int64]chan TxEvent
}

func OpenEngine(dataDir string, treeNames []string) (*Engine, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	eng := &Engine{
		dir:      dataDir,
		trees:    make(map[string]*BTree),
		index:    NewIndexManager(),
		watchers: make(map[int64]chan TxEvent),
	}

	if err := eng.loadSnapshot(); err != nil {
		return nil, err
	}
	for _, n := range treeNames {
		if _, ok := eng.trees[n]; !ok {
			eng.trees[n] = NewBTree()
		}
	}

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
	return e.tx(fn, true)
}

func (e *Engine) TxSilent(fn func(tx *Tx) error) error {
	return e.tx(fn, false)
}

func (e *Engine) tx(fn func(tx *Tx) error, notify bool) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return fmt.Errorf("engine closed")
	}
	tx := &Tx{mutations: make([]Mutation, 0, 8)}
	if err := fn(tx); err != nil {
		e.mu.Unlock()
		return err
	}
	if len(tx.mutations) == 0 {
		e.mu.Unlock()
		return nil
	}

	for _, m := range tx.mutations {
		tree := e.mustTreeUnlocked(m.Tree)
		combined := makeTreeKey(m.Tree, m.Key)
		if err := e.wal.Append(m.Op, combined, m.Value); err != nil {
			e.mu.Unlock()
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
	if notify {
		e.sequence++
		evt := TxEvent{
			Sequence:  e.sequence,
			Timestamp: time.Now().UTC(),
			Mutations: cloneMutations(tx.mutations),
		}
		for _, watcher := range e.watchers {
			select {
			case watcher <- evt:
			default:
			}
		}
	}
	e.mu.Unlock()
	return nil
}

func (e *Engine) WatchTx() (id int64, ch <-chan TxEvent, unsubscribe func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		c := make(chan TxEvent)
		close(c)
		return 0, c, func() {}
	}
	e.nextWatchID++
	id = e.nextWatchID
	c := make(chan TxEvent, 32)
	e.watchers[id] = c
	return id, c, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if sub, ok := e.watchers[id]; ok {
			delete(e.watchers, id)
			close(sub)
		}
	}
}

func (e *Engine) CurrentSequence() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.sequence
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

func (e *Engine) RestoreSnapshot(path string) error {
	trees, err := ReadSnapshot(path)
	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("engine closed")
	}

	merged := make(map[string]*BTree, len(e.trees)+len(trees))
	for name := range e.trees {
		merged[name] = NewBTree()
	}
	for name, tree := range trees {
		merged[name] = tree
	}

	if err := WriteSnapshot(filepath.Join(e.dir, "snapshot.bin"), merged); err != nil {
		return err
	}

	if e.wal != nil {
		if err := e.wal.Close(); err != nil {
			return err
		}
	}
	walDir := filepath.Join(e.dir, "wal")
	if err := os.RemoveAll(walDir); err != nil {
		return err
	}
	wal, err := OpenWAL(walDir)
	if err != nil {
		return err
	}

	e.trees = merged
	e.wal = wal
	return nil
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
	for id, watcher := range e.watchers {
		close(watcher)
		delete(e.watchers, id)
	}
	var errs []error
	if e.wal != nil {
		if err := e.wal.Close(); err != nil {
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

func cloneMutations(in []Mutation) []Mutation {
	out := make([]Mutation, 0, len(in))
	for _, item := range in {
		out = append(out, Mutation{
			Tree:  item.Tree,
			Op:    item.Op,
			Key:   append([]byte(nil), item.Key...),
			Value: append([]byte(nil), item.Value...),
		})
	}
	return out
}
