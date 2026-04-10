package storage

import "errors"

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

var (
	ErrNotFound = errors.New("not found")
)
