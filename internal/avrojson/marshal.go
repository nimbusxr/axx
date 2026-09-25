package avrojson

import (
	"github.com/iskorotkov/avro/v2"
)

// javaAPI writes arrays and maps as Java's GenericDatumWriter does: one
// block holding every item, prefixed by the item count only.
var javaAPI = avro.Config{DisableBlockSizeHeader: true, BlockLength: 1 << 30}.Freeze()

// Marshal writes v, a value in this package's representation, in Avro's
// binary encoding. The bytes are those Java writes for the same datum,
// except that map entries follow Go's map order instead of Java's.
func Marshal(s avro.Schema, v any) ([]byte, error) {
	return javaAPI.Marshal(s, v)
}
