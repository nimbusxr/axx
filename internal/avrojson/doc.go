// Package avrojson reads and writes Avro values the way the Apache Avro Java
// library does, for the steps and tools that exchange Avro data with people:
// payload files written in Avro's JSON encoding, and assertions over the text
// Java printed for a record.
//
// # Values
//
// Decode, DecodeBinary and Render share one representation of Avro values,
// the one github.com/iskorotkov/avro/v2 uses for generic data, so a decoded value
// can be passed straight to avro.Marshal:
//
//	null            nil
//	boolean         bool
//	int             int      (time-millis and date stay plain ints)
//	long            int64    (timestamps stay plain int64s; time-micros is a time.Duration)
//	float, double   float32, float64
//	bytes           []byte   (decimal stays the raw two's-complement bytes)
//	string          string   (uuid too)
//	enum            string   (the symbol)
//	fixed           [N]byte
//	array           []any
//	map             map[string]any (DecodeBinary: OrderedMap)
//	record          map[string]any, keyed by field name
//	union           nil for the null branch, otherwise map[string]any{"<branch>": value}
//
// Logical types keep their underlying representation, exactly as Java's
// GenericDatumReader returns them when no conversions are registered (the
// default for GenericData and the Confluent deserializers). The union branch
// key is the avro library's type name: the full name of a named type, otherwise the type
// name with the logical type appended ("long.timestamp-millis"). Render also
// accepts the avro library's converted forms (time.Time, time.Duration, *big.Rat,
// avro.LogicalDuration) and bare union values.
//
// # JSON encoding
//
// Decode follows org.apache.avro.io.JsonDecoder (Avro 1.12), which the Java
// framework used to read payload files, including its quirks:
//
//   - Record fields may appear in any order; unknown fields are ignored; when
//     a field appears twice the first one wins. Every field must be present:
//     the JSON decoder does not apply schema defaults.
//   - A union value is null or a single-entry object {"<branch>": value}
//     whose key is the full name of a named type or the name of any other
//     type ("string", "long", "array", "map", ...; never a logical type
//     name). Only the first entry is read. Options.LenientUnions also accepts
//     a bare value when exactly one branch can decode it.
//   - bytes and fixed values are strings whose characters are the byte
//     values (ISO-8859-1); other characters become '?'.
//   - int accepts a decimal literal whose float value is integral ("1.0",
//     "1e2"), long one whose double value is; float and double accept any
//     number and the strings "NaN", "Infinity" and "-Infinity".
//   - Map keys that repeat keep the last value; text after the first JSON
//     value is ignored.
//
// Errors are *Error values naming the JSONPath of the offending value.
//
// # Rendering
//
// Render reproduces GenericData.toString, which is what a Java
// GenericRecord's toString() returns: {"field": value, ...} with ", " and
// ": " separators, strings and enum symbols quoted with Avro's escaping,
// bytes as an ISO-8859-1 string, fixed as a signed byte list ([-1, 0, 97]),
// floats and doubles as Java prints them (NaN and infinities quoted), unions
// without their wrapper, and map entries in java.util.HashMap iteration
// order. ObjectString is the Object.toString() of a top-level value, which
// differs from Render for anything but records (a string prints unquoted).
package avrojson
