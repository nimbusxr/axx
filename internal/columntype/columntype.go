// Package columntype holds a table's cell to the type of the column it picks
// rows by (ADR 0014). A database that knows its columns' types converts between
// them when it compares (MySQL's `reference = 0` matches every text that is no
// number, SQLite's `postcode = 10115` the text '10115', PostgreSQL's
// `weight = 850.0` the integer 850), so a pack checks the cell first, against
// the type its driver gives, and fails with the explicit form.
package columntype

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// Kind is what a column's type holds, as a cell writes it.
type Kind int

const (
	// Other is any type written as a string: dates, times, UUIDs, JSON, enums,
	// binary.
	Other Kind = iota
	// Integer holds integers: `850`, `850L`.
	Integer
	// Decimal holds decimals: `850.0`, `1.5`, `1e3`.
	Decimal
	// Boolean holds `true` and `false`.
	Boolean
	// Text holds strings: `PX-1`, `"10115"`.
	Text
	// IntegerOrBoolean is an integer type a database's booleans are stored in
	// (MySQL's TINYINT).
	IntegerOrBoolean
)

// Column is a column of a known type.
type Column struct {
	// Name is the column as the step's table names it.
	Name string
	// Table is the table as the step names it.
	Table string
	// Type is the type's name, as the database gives it.
	Type string
	Kind Kind
}

// Wrong says what to write when v is not of the column's type: `postcode is text
// in parcels.pickups (VARCHAR): write "10115"`, and "" when it is. null, which is
// how any column has no value, and JSON, which a step gives the database as it
// is, are not held to it.
func (c Column) Wrong(v core.Value) string {
	write := c.write(v)
	if write == "" {
		return ""
	}
	return fmt.Sprintf("%s %s in %s (%s): write %s", c.Name, c.is(), c.Table, c.Type, write)
}

// Fail is the step's error for what Wrong said of its values, all of them at
// once, as `axx validate` reports a value: nil when there is nothing wrong.
func Fail(wrong []string) error {
	if len(wrong) == 0 {
		return nil
	}
	return core.NewValueError(strings.Join(wrong, "; "))
}

func (c Column) is() string {
	switch c.Kind {
	case Integer:
		return "is an integer"
	case Decimal:
		return "is a decimal"
	case Boolean:
		return "is a boolean"
	case Text:
		return "is text"
	case IntegerOrBoolean:
		return "is an integer or a boolean"
	}
	return "is written as a string"
}

// write is what the column takes for v, or "" when v is of its type.
func (c Column) write(v core.Value) string {
	cell := classOf(v)
	if cell == unchecked {
		return ""
	}
	switch c.Kind {
	case Integer, IntegerOrBoolean:
		return c.integer(v, cell)
	case Decimal:
		return decimal(v, cell)
	case Boolean:
		return boolean(v, cell)
	}
	if cell == stringCell {
		return ""
	}
	return `"` + v.Text() + `"`
}

func (c Column) integer(v core.Value, cell class) string {
	switch cell {
	case integerCell:
		return ""
	case booleanCell:
		if c.Kind == IntegerOrBoolean {
			return ""
		}
	case decimalCell:
		if whole, ok := wholeNumber(v.JSON()); ok {
			return whole
		}
	case stringCell:
		inner, ok := readString(v)
		if !ok {
			break
		}
		switch classOf(inner) {
		case integerCell:
			return inner.JSON()
		case decimalCell:
			if whole, ok := wholeNumber(inner.JSON()); ok {
				return whole
			}
		case booleanCell:
			if c.Kind == IntegerOrBoolean {
				return inner.JSON()
			}
		}
	}
	return "an integer, not " + v.Text()
}

func decimal(v core.Value, cell class) string {
	switch cell {
	case decimalCell:
		return ""
	case integerCell:
		return v.JSON() + ".0"
	case stringCell:
		if inner, ok := readString(v); ok {
			switch classOf(inner) {
			case decimalCell:
				return inner.JSON()
			case integerCell:
				return inner.JSON() + ".0"
			}
		}
	}
	return "a decimal, not " + v.Text()
}

func boolean(v core.Value, cell class) string {
	switch cell {
	case booleanCell:
		return ""
	case integerCell:
		switch v.JSON() {
		case "1":
			return "true"
		case "0":
			return "false"
		}
	case stringCell:
		if inner, ok := readString(v); ok && classOf(inner) == booleanCell {
			return inner.JSON()
		}
	}
	return "true or false, not " + v.Text()
}

// Checked says the value is one a column's type decides: not null, which is
// how any column has no value, and not JSON, which a step gives the database as
// it is.
func Checked(v core.Value) bool { return classOf(v) != unchecked }

type class int

const (
	unchecked class = iota
	stringCell
	integerCell
	decimalCell
	booleanCell
)

func classOf(v core.Value) class {
	if _, ok := v.Str(); ok {
		return stringCell
	}
	switch j := v.JSON(); {
	case v.IsNull(), v.Undefined(), strings.HasPrefix(j, "{"), strings.HasPrefix(j, "["):
		return unchecked
	case j == "true", j == "false":
		return booleanCell
	case v.IsDecimal():
		return decimalCell
	}
	return integerCell
}

// readString is what a string's text would read as without its quotes: the
// value someone who wrote `"850"` meant.
func readString(v core.Value) (core.Value, bool) {
	s, _ := v.Str()
	inner, err := core.ParseValue(s, false, false)
	return inner, err == nil
}

// maxWritten is the longest whole number a message spells out: `1e999` is one
// too, of a thousand digits.
const maxWritten = 40

// wholeNumber is a decimal's value as an integer, when it is whole.
func wholeNumber(text string) (string, bool) {
	r, ok := new(big.Rat).SetString(text)
	if !ok || !r.IsInt() {
		return "", false
	}
	s := r.Num().String()
	return s, len(s) <= maxWritten
}
