package gguf

import (
	"maps"
	"slices"
)

// Matrix is a matrix of few values: a stub's, laid out in a target's
// architecture, whose matrices are mostly 0 (a real model's sizes, of a
// stub's few features). It has its rows' values, by their columns, after
// the values every row has (Base: the ballast's, of the token embeddings).
type Matrix struct {
	Rows, Cols int
	Base       map[int]float32
	rows       map[int]map[int]float32
}

// NewMatrix is a matrix of rows × cols 0s.
func NewMatrix(rows, cols int) *Matrix {
	return &Matrix{Rows: rows, Cols: cols, rows: map[int]map[int]float32{}}
}

// Add adds v to the value at row r, column c.
func (m *Matrix) Add(r, c int, v float32) {
	row := m.rows[r]
	if row == nil {
		row = map[int]float32{}
		m.rows[r] = row
	}
	row[c] += v
}

// Set sets the value at row r, column c.
func (m *Matrix) Set(r, c int, v float32) {
	m.Add(r, c, v-m.rows[r][c])
}

// At is the value at row r, column c.
func (m *Matrix) At(r, c int) float32 {
	if v, ok := m.rows[r][c]; ok {
		return v
	}
	return m.Base[c]
}

// Empty reports whether row r is 0s.
func (m *Matrix) Empty(r int) bool {
	for _, v := range m.Base {
		if v != 0 {
			return false
		}
	}
	for _, v := range m.rows[r] {
		if v != 0 {
			return false
		}
	}
	return true
}

// ScaleCol multiplies column c's values by f.
func (m *Matrix) ScaleCol(c int, f float32) {
	for _, row := range m.rows {
		if v, ok := row[c]; ok {
			row[c] = v * f
		}
	}
	if v, ok := m.Base[c]; ok {
		m.Base[c] = v * f
	}
}

// Compensate makes each column's values sum to 0, of n rows from at, each
// of an n-th of what the column's other rows add up to, taken away.
func (m *Matrix) Compensate(at, n int) {
	sums := map[int]float64{}
	for r, row := range m.rows {
		if r >= at && r < at+n {
			continue
		}
		for c, v := range row {
			sums[c] += float64(v)
		}
	}
	for c, sum := range sums {
		if sum != 0 {
			for r := at; r < at+n; r++ {
				m.Set(r, c, float32(-sum/float64(n)))
			}
		}
	}
}

// Row is row r's values, in dst (of Cols values, or a new slice).
func (m *Matrix) Row(r int, dst []float32) []float32 {
	if len(dst) != m.Cols {
		dst = make([]float32, m.Cols)
	} else {
		clear(dst)
	}
	for c, v := range m.Base {
		dst[c] = v
	}
	for c, v := range m.rows[r] {
		dst[c] = v
	}
	return dst
}

// Entry is a value of a row, and its column.
type Entry struct {
	Col int
	V   float32
}

// Entries are row r's values that are not 0, by their columns.
func (m *Matrix) Entries(r int) []Entry {
	cols := map[int]float32{}
	maps.Copy(cols, m.Base)
	maps.Copy(cols, m.rows[r])
	var out []Entry
	for _, c := range slices.Sorted(maps.Keys(cols)) {
		if cols[c] != 0 {
			out = append(out, Entry{c, cols[c]})
		}
	}
	return out
}

// Stack is the matrices' rows, one's after another's, of each's rows in
// the order of order (its row i, order's i: all of them, when nil).
func Stack(order func(r int) int, ms ...*Matrix) *Matrix {
	out := NewMatrix(0, ms[0].Cols)
	for _, m := range ms {
		for r, row := range m.rows {
			to := r
			if order != nil {
				to = order(r)
			}
			for c, v := range row {
				out.Add(out.Rows+to, c, v)
			}
		}
		out.Rows += m.Rows
	}
	return out
}
