// Package v8cov turns the JavaScript coverage V8 counts into Istanbul's
// coverage and reports, as Node's tools do, byte for byte: it merges the
// coverage of a script taken several times as @bcoe/v8-coverage 1.0.2 does,
// converts a script, through its source map if it has one, as
// v8-to-istanbul 9.3.0 does, and merges, sums up and writes files as
// istanbul-lib-coverage 3.2.2 and istanbul-reports 3.2.0 do (lcovonly,
// json and json-summary). testdata holds what those tools make of recorded
// coverage.
//
// Paths are slash-separated, as URL paths are: a script's path is its URL
// path, and the original files of its source map resolve against it.
package v8cov

// Range is a range of a script and how many times it ran; offsets are
// UTF-16 code units of the script's source (JavaScript string indices), not
// bytes.
type Range struct {
	StartOffset int `json:"startOffset"`
	EndOffset   int `json:"endOffset"`
	Count       int `json:"count"`
}

// Function is a function's coverage, as V8 reports it: the function's own
// range first, then its blocks.
type Function struct {
	FunctionName    string  `json:"functionName"`
	Ranges          []Range `json:"ranges"`
	IsBlockCoverage bool    `json:"isBlockCoverage"`
}
