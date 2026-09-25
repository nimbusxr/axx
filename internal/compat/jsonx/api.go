package jsonx

import (
	"strings"
)

// Parse parses text like Jayway's JsonPath.parse(String), i.e. with
// json-smart in permissive mode. Numbers become Integer, Long, BigInteger,
// Double or BigDecimal exactly as json-smart classifies them. Empty text and
// the text "null" are rejected (IllegalArgument), as Jayway rejects them;
// malformed text yields InvalidJSON. Lenient input (single quotes, unquoted
// strings, trailing garbage after the first value, ...) is accepted like
// json-smart accepts it.
func Parse(text string) (any, error) {
	if text == "" {
		return nil, newErr(IllegalArgument, "json string can not be null or empty")
	}
	v, err := parseJSONPathText(text)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, newErr(IllegalArgument, "json can not be null")
	}
	return v, nil
}

// parseJSONPathText is JsonSmartJsonProvider.parse.
func parseJSONPathText(text string) (any, *Error) {
	v, perr := parseSmart(text)
	if perr != nil {
		return nil, newErr(InvalidJSON, "net.minidev.json.parser.ParseException: %s", perr.message())
	}
	return v, nil
}

// Marshal serializes doc like DocumentContext.jsonString(): compact JSON as
// json-smart writes it (Double.toString numbers, infinite doubles as null,
// U+007F-U+009F and U+2000-U+20FF escaped). A string root cannot be
// serialized (UnsupportedOperation), as in Java.
func Marshal(doc any) (string, error) {
	switch doc.(type) {
	case nil:
		return "", &Error{Kind: NullPointer, Message: `Cannot invoke "Object.getClass()" because "obj" is null`}
	case *Object, *Array, []any, Number, bool:
	default:
		return "", newErr(UnsupportedOperation, "%s can not be converted to JSON", JavaClassName(doc))
	}
	var sb strings.Builder
	writeValue(&sb, doc, styleLTCompress)
	return sb.String(), nil
}

// MarshalValue serializes any value (including a string) as json-smart's
// JSONValue.toJSONString does with its default style.
func MarshalValue(v any) string {
	var sb strings.Builder
	writeValue(&sb, v, styleNoCompress)
	return sb.String()
}

// Path is a compiled JSONPath expression.
type Path struct {
	text string
	cp   *compiledPath
}

// Compile compiles path like JsonPath.compile. Paths that do not start with
// '$' or '@' are relative to the root ("name" means "$.name").
func Compile(path string) (*Path, error) {
	if path == "" {
		return nil, newErr(IllegalArgument, "json can not be null or empty")
	}
	var cp *compiledPath
	if err := catch(func() { cp = compilePath(path) }); err != nil {
		return nil, err
	}
	return &Path{text: path, cp: cp}, nil
}

// Definite reports whether the path can match at most one location (no
// wildcards, deep scans, filters, slices or index lists).
func (p *Path) Definite() bool { return p.cp.isDefinite() }

// String returns Jayway's normalized form of the path, e.g. "$['a'][0]".
func (p *Path) String() string { return p.cp.String() }

// Read evaluates path against doc like DocumentContext.read(path). A definite
// path yields the single matching value (PathNotFound when it does not
// exist); an indefinite path yields an *Array of every match, possibly empty.
// Values are shared with doc, not copied.
func Read(doc any, path string) (value any, definite bool, err error) {
	if path == "" {
		return nil, false, newErr(IllegalArgument, "path can not be null or empty")
	}
	p, err := Compile(path)
	if err != nil {
		return nil, false, err
	}
	value, err = p.read(doc)
	return value, p.Definite(), err
}

// ReadPaths evaluates path against doc like Read with Jayway's
// Option.AS_PATH_LIST, and also returns what each location holds: paths[i]
// is the normalized path of a match ("$['a'][0]['id']", keys not escaped)
// and values[i] its value, in evaluation order. Unlike Jayway, a path that
// matches nothing yields empty slices instead of PathNotFound; invalid paths
// and other evaluation errors are returned.
func ReadPaths(doc any, path string) (paths []string, values []any, err error) {
	p, err := Compile(path)
	if err != nil {
		return nil, nil, err
	}
	if e := catch(func() {
		ctx := p.cp.evaluate(doc, doc, false, evalOptions{})
		paths, values = ctx.paths, ctx.values
	}); e != nil {
		if e.Kind.Is(PathNotFound) {
			return nil, nil, nil
		}
		return nil, nil, e
	}
	return paths, values, nil
}

func (p *Path) read(doc any) (value any, err error) {
	if e := catch(func() {
		value = p.cp.evaluate(doc, doc, false, evalOptions{}).getValue()
	}); e != nil {
		return nil, e
	}
	return value, nil
}

// Set replaces every location path matches with value, like
// DocumentContext.set. When nothing matches it fails with PathNotFound (and
// no message). Setting the root is an InvalidModification.
func Set(doc any, path string, value any) error {
	return update(doc, path, func(r pathRef) { r.set(value) })
}

// Delete removes every location path matches, like DocumentContext.delete.
func Delete(doc any, path string) error {
	return update(doc, path, func(r pathRef) { r.delete() })
}

// Put sets member key of every object parentPath matches, like
// DocumentContext.put. Non-object matches are an InvalidModification; null
// matches are skipped.
func Put(doc any, parentPath, key string, value any) error {
	if parentPath == "" {
		return newErr(IllegalArgument, "json can not be null or empty")
	}
	p, err := Compile(parentPath)
	if err != nil {
		return err
	}
	if key == "" {
		return newErr(IllegalArgument, "key can not be null or empty")
	}
	return p.update(doc, func(r pathRef) { r.put(key, value) })
}

func update(doc any, path string, apply func(pathRef)) error {
	p, err := Compile(path)
	if err != nil {
		return err
	}
	return p.update(doc, apply)
}

func (p *Path) update(doc any, apply func(pathRef)) error {
	if e := catch(func() {
		ctx := p.cp.evaluate(doc, doc, true, evalOptions{})
		if len(ctx.paths) == 0 {
			throwNoMessage(PathNotFound)
		}
		for _, op := range ctx.updateOperations() {
			apply(op)
		}
	}); e != nil {
		return e
	}
	return nil
}

// Exists reports whether reading path from doc succeeds, i.e. the negation
// of json-path-assert's hasNoJsonPath: any JsonPath exception while reading
// means "does not exist". Note that an indefinite path always exists (it
// reads as a possibly empty list). Invalid paths and non-JsonPath failures
// are returned as errors.
func Exists(doc any, path string) (bool, error) {
	p, err := Compile(path)
	if err != nil {
		return false, err
	}
	if _, err := p.read(doc); err != nil {
		if IsJsonPathException(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// HasJSONPath is json-path-assert's hasJsonPath(path, matcher) applied to a
// JSON text: body must parse and path must read successfully, and then match
// decides. Unparsable bodies and failing reads (JsonPath exceptions) are "no
// match"; invalid paths and other failures (an empty body, the body "null")
// are errors, as they are exceptions in Java.
func HasJSONPath(body, path string, match func(value any) bool) (bool, error) {
	p, err := Compile(path)
	if err != nil {
		return false, err
	}
	doc, err := Parse(body)
	if err != nil {
		if IsJsonPathException(err) {
			return false, nil
		}
		return false, err
	}
	v, err := p.read(doc)
	if err != nil {
		if IsJsonPathException(err) {
			return false, nil
		}
		return false, err
	}
	return match(v), nil
}

// HasNoJSONPath is json-path-assert's hasNoJsonPath(path) applied to a JSON
// text: it holds when reading path fails with a JsonPath exception. An
// unparsable body does not hold.
func HasNoJSONPath(body, path string) (bool, error) {
	p, err := Compile(path)
	if err != nil {
		return false, err
	}
	doc, err := Parse(body)
	if err != nil {
		if IsJsonPathException(err) {
			return false, nil
		}
		return false, err
	}
	if _, err := p.read(doc); err != nil {
		if IsJsonPathException(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// Normalize returns path with an explicit root, the way Jayway reads it: a
// path not starting with '$' or '@' is relative to the root, so "name"
// becomes "$.name" and "[0].name" becomes "$[0].name". Normalize does not
// validate the path; Read(doc, p) and Read(doc, Normalize(p)) behave the
// same, except that error positions of relative paths shift.
func Normalize(path string) string {
	if path == "" {
		return path
	}
	switch path[0] {
	case '$', '@':
		return path
	case '[':
		return "$" + path
	}
	return "$." + path
}

// ParentAndKey splits a property path at its last '.', exactly like the REST
// request step's extractParentPath/extractKey: "address.city" gives
// ("$.address", "city") and "name" gives ("$", "name"). Brackets are not
// interpreted, so "a[0]" gives ("$", "a[0]").
func ParentAndKey(path string) (parent, key string) {
	normalized := path
	if !strings.HasPrefix(path, "$.") {
		normalized = "$." + path
	}
	lastDot := strings.LastIndexByte(normalized, '.')
	parent = "$"
	if lastDot > 1 {
		parent = normalized[:lastDot]
	}
	return parent, normalized[lastDot+1:]
}
