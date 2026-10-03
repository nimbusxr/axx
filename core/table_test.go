package core

import (
	"reflect"
	"testing"
)

func TestWithoutNamesRow(t *testing.T) {
	doc := &TableDoc{Columns: []string{"JSONPath", "value"}}
	table := func(rows ...[]string) *Table { return &Table{Rows: rows} }
	for name, c := range map[string]struct {
		doc  *TableDoc
		in   *Table
		want [][]string
	}{
		"names row dropped":           {doc, table([]string{"JSONPath", "value"}, []string{"$.a", "1"}), [][]string{{"$.a", "1"}}},
		"case and spaces ignored":     {doc, table([]string{" jsonpath ", "VALUE"}, []string{"$.a", "1"}), [][]string{{"$.a", "1"}}},
		"no names row":                {doc, table([]string{"$.a", "1"}), [][]string{{"$.a", "1"}}},
		"other first row":             {doc, table([]string{"JSONPath", "1"}), [][]string{{"JSONPath", "1"}}},
		"a row of another width":      {doc, table([]string{"JSONPath", "value", "x"}), [][]string{{"JSONPath", "value", "x"}}},
		"a step without column names": {&TableDoc{}, table([]string{"JSONPath", "value"}), [][]string{{"JSONPath", "value"}}},
		"no doc":                      {nil, table([]string{"JSONPath", "value"}), [][]string{{"JSONPath", "value"}}},
		"a one-column table":          {&TableDoc{Columns: []string{"option"}}, table([]string{"Option"}, []string{"Express"}), [][]string{{"Option"}, {"Express"}}},
	} {
		got := c.doc.WithoutNamesRow(c.in)
		if !reflect.DeepEqual(got.Rows, c.want) {
			t.Errorf("%s: got %v, want %v", name, got.Rows, c.want)
		}
	}
	if (*TableDoc)(nil).WithoutNamesRow(nil) != nil || doc.WithoutNamesRow(nil) != nil {
		t.Error("a missing table should stay missing")
	}
}
