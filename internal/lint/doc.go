// Package lint implements `axx lint`: test-data isolation rules plus
// builtin feature-file checks.
//
// # Rules
//
// The lint section of axx.yaml (config.Lint) holds rules, an optional
// config block (baseDir, mode, maxFileSize, maxReportedValues,
// maxReportedLocations) and includes. Each rule selects files with
// filePatterns minus excludePatterns (globs, see glob.go), extracts values
// from them, and reports values that repeat where they must be unique:
//
//   - global-unique (default): a value may occur once in all the files;
//   - file-unique: no value may repeat within a file;
//   - cross-file-unique: repeats within one file are fine, but no two files
//     may share a value.
//
// Values come from a Java regular expression (compat/javare, MULTILINE, the
// first participating capture group of every match, located at the captured
// text) or, for `type: jsonpath`, from the scalar values at a JSON path (see
// json.go). ignoreValues are never reported. A rule in warn mode (its own
// mode, else config.mode) reports its duplicates as warnings; files that
// cannot be read or parsed are errors in either mode.
// Files over maxFileSize are skipped with a warning and binary files (a NUL
// byte in the first 8 KiB) silently; a pattern whose directory does not exist
// matches nothing, so rules over generated fixtures pass before
// `axx fixtures generate` has run.
//
// Includes resolve relative to the including file and contribute rules (and
// further includes) only; a file included twice is reported as a cycle.
// Every configuration problem is reported at once, with its file and line:
// unknown keys or enum values, a regex without a capture group, an empty
// filePatterns list, and a baseDir that is not a directory.
//
// Locations point at the captured value (line and column). JSON paths are
// evaluated like Jayway JsonPath. The walk skips .git and .axx directories.
// Human output truncates long lists of values and locations; the machine
// formats (json, junit, sarif, github) list every finding.
//
// # Builtin checks
//
// CheckFeatures warns about SQL selection and trigger ordinals that cannot
// work (see features.go). It runs over the features in run.paths.
package lint
