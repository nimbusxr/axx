// Package javafmt reproduces how Java renders and parses numbers and values,
// so text axx produces matches what Java produces for the same value.
//
// Formatting:
//   - [Double] and [Float] are Double.toString and Float.toString: the
//     shortest decimal that round-trips (JDK 19+ algorithm), rendered as
//     "1.0", "0.001", "1.0E10" or "1.0E-5".
//   - [ValueOf] is String.valueOf: "null" for nil, "{a=1, b=[1, 2]}" for maps
//     and lists, and whatever a value's JavaString method returns for types
//     that model a specific Java class (the jsonx model uses this so a
//     json-smart JSONArray prints as JSON, exactly like Java).
//
// Parsing:
//   - [ParseInt], [ParseLong], [ParseDouble], [ParseBoolean] and
//     [ParseBigInteger] accept exactly what Integer.parseInt, Long.parseLong,
//     Double.parseDouble, Boolean.parseBoolean and new BigInteger(String)
//     accept, including Java's leniencies (a leading '+', Unicode decimal
//     digits for the integer parsers, surrounding whitespace, "NaN",
//     "Infinity", hexadecimal significands and f/d suffixes for doubles).
//   - [BigDecimal] models java.math.BigDecimal text, comparison and equality.
//
// The oracle in testdata/oracles/javafmt.json pins all of this to the
// behavior of Java 21.
package javafmt
