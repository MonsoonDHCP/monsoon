package storage

type Iterator struct {
	rows [][2][]byte
	idx  int
}

func NewIterator(tree *BTree, start []byte, end []byte, reverse bool) *Iterator {
	rows := make([][2][]byte, 0, 1024)
	tree.Iterate(start, end, func(k, v []byte) bool {
		rows = append(rows, [2][]byte{k, v})
		return true
	})
	if reverse {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	return &Iterator{rows: rows}
}

func (it *Iterator) Next() bool {
	if it.idx >= len(it.rows) {
		return false
	}
	it.idx++
	return true
}

func (it *Iterator) Key() []byte {
	if it.idx == 0 || it.idx > len(it.rows) {
		return nil
	}
	return append([]byte(nil), it.rows[it.idx-1][0]...)
}

func (it *Iterator) Value() []byte {
	if it.idx == 0 || it.idx > len(it.rows) {
		return nil
	}
	return append([]byte(nil), it.rows[it.idx-1][1]...)
}

func (it *Iterator) Seek(key []byte) {
	it.idx = 0
	for i, row := range it.rows {
		if string(row[0]) >= string(key) {
			it.idx = i
			return
		}
	}
	it.idx = len(it.rows)
}
