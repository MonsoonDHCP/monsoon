package storage

import (
	"errors"
	"time"
)

type OpType byte

const (
	OpPut OpType = 1
	OpDel OpType = 2
)

type Mutation struct {
	Tree  string
	Op    OpType
	Key   []byte
	Value []byte
}

type TxEvent struct {
	Sequence  int64
	Timestamp time.Time
	Mutations []Mutation
}

var (
	ErrNotFound = errors.New("not found")
)
