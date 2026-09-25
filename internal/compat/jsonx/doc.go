// Package jsonx reads, queries and modifies JSON exactly as Jayway JsonPath
// 2.9.0 does on top of json-smart 2.5.2, in their default configuration.
//
// # Model
//
// Parse reads text as json-smart does in the permissive mode Jayway uses
// (single-quoted and unquoted strings, NaN, leading zeros, redundant commas
// and trailing garbage are all accepted, exactly as far as json-smart accepts
// them). Objects keep their member order (*Object is a LinkedHashMap) and
// every number keeps the Java class json-smart gave it (see NumberKind):
// Integer when it fits 32 bits, Long, BigInteger past 64 bits, Double for
// decimals, BigDecimal for decimal literals longer than 18 characters. That
// class matters: Java's equals never considers the Integer 5 equal to the
// Double 5.0, and Marshal writes numbers back the way Java prints them
// ("1e10" comes back as "1.0E10"). Number.Text keeps the original literal.
//
// Marshal is DocumentContext.jsonString(): compact JSON with json-smart's
// escaping.
//
// # Paths
//
// Read, Set, Put, Delete and Exists evaluate Jayway JsonPath expressions,
// including every Jayway quirk the oracle exercises: a path not starting
// with '$' or '@' is relative to the root ("name", "[0].name"); definite
// paths return a value and fail with PathNotFound when it is missing, while
// indefinite paths (wildcards, deep scans, filters, slices, index lists)
// return a possibly empty list; filters support ==, !=, ===, !==, <, <=, >,
// >=, =~ (Java regular expressions with /.../idmsxuU flags), in, nin,
// subsetof, anyof, noneof, contains, size, empty, && || ! and path functions
// such as length(); negative indexes read from the end but fail on update.
// Errors are *Error values whose Kind names the Java exception
// (PathNotFound, InvalidPath, InvalidModification, ...), with Java's message.
//
// # Why a port instead of a library
//
// The evaluator is a port of Jayway's own classes (PathCompiler, the path
// tokens, FilterCompiler, the value nodes and evaluators, PathRef) rather than
// an existing Go JSONPath library. github.com/ohler55/ojg (jp package, with
// ordered maps through jp.Keyed) was evaluated against the same oracle: even
// comparing values loosely, ignoring Java number classes, it agreed with
// Jayway on only 68% of the read cases. It has no Jayway path functions
// (length(), sum(), keys(), ...), no nin/size/empty/subsetof operators and
// no regex flags, merges multi-property selections differently, orders deep
// scans differently, does not coerce between strings and numbers in filters
// the way Jayway does, and knows nothing of Jayway's error and update
// semantics. A port reproduces all of it by construction, and the oracle
// (testdata/oracles/jsonpath.json) confirms it case by case.
package jsonx
