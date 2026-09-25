package rest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	vconfig "github.com/pb33f/libopenapi-validator/config"
	liberrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nimbusxr/axx/core"
)

// spec is a parsed OpenAPI document and its validator, shared by every
// scenario of the run.
type spec struct {
	location string // URL or absolute file path
	model    *v3.Document

	mu        sync.Mutex // serializes validation
	validator validator.Validator
}

// specCache parses each specification once per run. Unlike axx.Cached it
// does not remember failures, so a specification served by an application
// that was still starting is fetched again by the next scenario.
type specCache struct {
	mu sync.Mutex
	m  map[string]*specEntry
}

type specEntry struct {
	mu   sync.Mutex
	spec *spec
}

func (c *specCache) get(key string, load func() (*spec, error)) (*spec, error) {
	c.mu.Lock()
	e := c.m[key]
	if e == nil {
		e = &specEntry{}
		c.m[key] = e
	}
	c.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.spec != nil {
		return e.spec, nil
	}
	s, err := load()
	if err != nil {
		return nil, err
	}
	e.spec = s
	return s, nil
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// resolveSpecLocation turns the openapi property into a URL or an absolute
// file path. File paths (optionally classpath:/file: prefixed) resolve
// against the configured resource roots.
func resolveSpecLocation(sc *core.Scenario, location string) (string, error) {
	if isHTTPURL(location) {
		return location, nil
	}
	p := strings.TrimPrefix(location, "file://")
	p = strings.TrimPrefix(p, "file:")
	resolved, err := sc.Suite().ResolvePath(p)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// loadSpec returns the parsed specification of svc.
func loadSpec(sc *core.Scenario, set *settings, svc *Service) (*spec, error) {
	loc, err := resolveSpecLocation(sc, svc.OpenAPI)
	if err != nil {
		return nil, fmt.Errorf("OpenAPI specification %s of service %s: %w", svc.OpenAPI, svc.Name, err)
	}
	return set.specs.get(loc, func() (*spec, error) {
		data, err := fetch(sc.Context(), set.client, loc)
		if err != nil {
			return nil, fmt.Errorf("could not read the OpenAPI specification of service %s: %w", svc.Name, err)
		}
		s, err := parseSpec(loc, data)
		if err != nil {
			return nil, fmt.Errorf("could not parse the OpenAPI specification %s of service %s: %w", loc, svc.Name, err)
		}
		return s, nil
	})
}

// fetch reads a URL (GET, 200 required) or a file.
func fetch(ctx context.Context, client *http.Client, loc string) ([]byte, error) {
	if !isHTTPURL(loc) {
		return os.ReadFile(loc)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", loc, resp.StatusCode)
	}
	return body, nil
}

// parseSpec parses an OpenAPI 3.0 or 3.1 document and builds its validator.
func parseSpec(loc string, data []byte) (*spec, error) {
	cfg := datamodel.NewDocumentConfiguration()
	if isHTTPURL(loc) {
		base, err := url.Parse(loc)
		if err != nil {
			return nil, err
		}
		dir := *base
		dir.Path = pathDir(base.Path)
		cfg.BaseURL = &dir
		cfg.AllowRemoteReferences = true
	} else {
		cfg.BasePath = filepath.Dir(loc)
		cfg.AllowFileReferences = true
	}
	doc, err := libopenapi.NewDocumentWithConfiguration(data, cfg)
	if err != nil {
		return nil, err
	}
	if v := doc.GetSpecInfo(); v != nil && v.SpecType != "" && v.SpecType != "openapi" {
		return nil, fmt.Errorf("only OpenAPI 3.x documents are supported (this one is %s %s)", v.SpecType, v.Version)
	}
	model, err := doc.BuildV3Model()
	if model == nil {
		if err == nil {
			err = errors.New("not an OpenAPI 3 document")
		}
		return nil, err
	}
	// Circular references and similar warnings still leave a usable model.
	v := validator.NewValidatorFromV3Model(&model.Model,
		vconfig.WithFormatAssertions(),
		vconfig.WithRegexEngine(ecmaRegexp),
		vconfig.WithRejectUndeclaredRequestBody(),
		vconfig.WithURLEncodedBodyValidation(),
	)
	return &spec{location: loc, model: &model.Model, validator: v}, nil
}

func pathDir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i+1]
	}
	return "/"
}

// ecmaRegexp compiles schema patterns as ECMA-262 regular expressions, the
// dialect OpenAPI specifies (Go's RE2 lacks lookarounds and backreferences).
func ecmaRegexp(s string) (jsonschema.Regexp, error) {
	re, err := regexp2.Compile(s, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = 5 * time.Second
	return ecmaRE{re}, nil
}

type ecmaRE struct{ re *regexp2.Regexp }

func (r ecmaRE) MatchString(s string) bool {
	ok, err := r.re.MatchString(s)
	return err == nil && ok
}

func (r ecmaRE) String() string { return r.re.String() }

// validationRequest rebuilds the sent request for the validator.
func validationRequest(ctx context.Context, ex *Exchange) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, ex.Method, ex.validationPath, bytes.NewReader(ex.RequestBody))
	if err != nil {
		return nil, err
	}
	req.Header = ex.RequestHeader.Clone()
	req.ContentLength = int64(len(ex.RequestBody))
	return req, nil
}

// validate checks an executed exchange against the specification and
// returns the findings (before levels are applied).
func (s *spec) validate(ctx context.Context, ex *Exchange) []Issue {
	req, err := validationRequest(ctx, ex)
	if err != nil {
		return []Issue{{Key: "validation.request.unknownError", Message: err.Error()}}
	}
	resp := &http.Response{
		StatusCode: ex.Status,
		Status:     strconv.Itoa(ex.Status) + " " + http.StatusText(ex.Status),
		Header:     ex.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(ex.Body)),
		Request:    req,
	}
	if len(ex.Body) == 0 {
		resp.Body = http.NoBody
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, reqErrs := s.validator.ValidateHttpRequestSync(req)
	issues := mapIssues(dirRequest, reqErrs, len(ex.RequestBody) > 0)
	for _, e := range reqErrs {
		// Without a matching operation there is nothing to check the
		// response against; the request finding says it all.
		if e != nil && (e.IsPathMissingError() || e.IsOperationMissingError() ||
			e.ValidationSubType == "missingOperation") {
			return issues
		}
	}
	// A fresh request: validating consumed the body.
	req2, err := validationRequest(ctx, ex)
	if err != nil {
		return issues
	}
	_, respErrs := s.validator.GetResponseBodyValidator().ValidateResponseBody(req2, resp)
	return append(issues, mapIssues(dirResponse, dropPathErrors(respErrs), len(ex.RequestBody) > 0)...)
}

func dropPathErrors(errs []*liberrors.ValidationError) []*liberrors.ValidationError {
	out := errs[:0:0]
	for _, e := range errs {
		if e != nil && !e.IsPathMissingError() && !e.IsOperationMissingError() {
			out = append(out, e)
		}
	}
	return out
}

// ---- examples ----

// requestExample returns the request body example for method and path
// (the step's path; its query string is ignored) with the media type
// contentType: the example called name, or the first one in document order
// when name is empty.
func (s *spec) requestExample(sc *core.Scenario, set *settings, method, path, contentType, name string) (string, error) {
	text, external, err := s.pickExample(method, path, contentType, name)
	if err != nil || external == "" {
		return text, err
	}
	b, err := s.external(sc, set, external)
	if err != nil {
		return "", fmt.Errorf("Failed to resolve external example: %w", err) //nolint:staticcheck // user-facing message
	}
	return string(b), nil
}

// pickExample finds the example and returns its value as JSON text, or its
// externalValue reference. The model is read under the specification lock:
// libopenapi builds schemas lazily.
func (s *spec) pickExample(method, path, contentType, name string) (text, external string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mt, err := s.mediaType(method, path, contentType)
	if err != nil {
		return "", "", err
	}
	var ex *base.Example
	switch {
	case name != "":
		if mt.Examples != nil {
			ex, _ = mt.Examples.Get(name)
		}
		if ex == nil {
			return "", "", fmt.Errorf("No example with name %q found for request content", name) //nolint:staticcheck // user-facing message
		}
	case mt.Examples != nil && mt.Examples.First() != nil && mt.Examples.First().Value() != nil:
		first := mt.Examples.First()
		name, ex = first.Key(), first.Value()
	case mt.Example != nil:
		// A single `example`.
		text, err = nodeJSON(mt.Example)
		return text, "", err
	default:
		return "", "", errors.New("No examples found for request content") //nolint:staticcheck // user-facing message
	}
	switch {
	case ex.Value != nil:
		text, err = nodeJSON(ex.Value)
		return text, "", err
	case ex.ExternalValue != "":
		return "", ex.ExternalValue, nil
	}
	return "", "", fmt.Errorf("No value or externalValue found for example with name %q", name) //nolint:staticcheck // user-facing message
}

// external reads an example's externalValue: an absolute URL, or a
// reference relative to the specification (a URL, or a file; a relative
// file that is not next to the specification is looked up in the resource
// roots).
func (s *spec) external(sc *core.Scenario, set *settings, ref string) ([]byte, error) {
	if isHTTPURL(ref) {
		return fetch(sc.Context(), set.client, ref)
	}
	if isHTTPURL(s.location) {
		base, err := url.Parse(s.location)
		if err != nil {
			return nil, err
		}
		r, err := url.Parse(ref)
		if err != nil {
			return nil, err
		}
		return fetch(sc.Context(), set.client, base.ResolveReference(r).String())
	}
	p := strings.TrimPrefix(strings.TrimPrefix(ref, "file://"), "file:")
	if !filepath.IsAbs(p) {
		cand := filepath.Join(filepath.Dir(s.location), filepath.FromSlash(p))
		if _, err := os.Stat(cand); err == nil {
			return os.ReadFile(cand)
		}
		resolved, err := sc.Suite().ResolvePath(p)
		if err != nil {
			return nil, err
		}
		p = resolved
	}
	return os.ReadFile(p)
}

// mediaType finds the request body media type of the operation matching
// method and path: paths are tried in document order, path parameters match
// by their schema type, and the first path that matches and has the
// operation wins.
func (s *spec) mediaType(method, path, contentType string) (*v3.MediaType, error) {
	if s.model.Paths == nil || s.model.Paths.PathItems == nil {
		return nil, errors.New("No path found for request to get example") //nolint:staticcheck // user-facing message
	}
	reqPath, _, _ := strings.Cut(path, "?")
	if u, err := url.Parse(reqPath); err == nil && u.IsAbs() {
		reqPath = u.Path
	}
	var matched bool
	var op *v3.Operation
	for pair := s.model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		item := pair.Value()
		if item == nil || !templateMatches(pair.Key(), item, method, reqPath) {
			continue
		}
		matched = true
		if op = operation(item, method); op != nil {
			break
		}
	}
	switch {
	case !matched:
		return nil, errors.New("No path found for request to get example") //nolint:staticcheck // user-facing message
	case op == nil:
		return nil, fmt.Errorf("no %s operation found for request to get example", strings.ToUpper(method))
	case op.RequestBody == nil:
		return nil, errors.New("No request body found for request to get example") //nolint:staticcheck // user-facing message
	case op.RequestBody.Content == nil:
		return nil, errors.New("No content found for request to get example") //nolint:staticcheck // user-facing message
	}
	if mt, ok := op.RequestBody.Content.Get(contentType); ok && mt != nil {
		return mt, nil
	}
	// Tolerate parameters on the declared media type (application/json;charset=UTF-8).
	for c := op.RequestBody.Content.First(); c != nil; c = c.Next() {
		if mediaTypeOf(c.Key()) == contentType && c.Value() != nil {
			return c.Value(), nil
		}
	}
	return nil, errors.New("No content type found for request to get example") //nolint:staticcheck // user-facing message
}

func operation(item *v3.PathItem, method string) *v3.Operation {
	switch strings.ToUpper(method) {
	case http.MethodGet:
		return item.Get
	case http.MethodPost:
		return item.Post
	case http.MethodPut:
		return item.Put
	case http.MethodDelete:
		return item.Delete
	case http.MethodPatch:
		return item.Patch
	case http.MethodHead:
		return item.Head
	case http.MethodOptions:
		return item.Options
	case http.MethodTrace:
		return item.Trace
	}
	return nil
}

var templateParam = regexp.MustCompile(`\{([^{}/]+)\}`)

// templateMatches reports whether the request path matches a path template.
// Each {name} matches one path segment of the parameter's schema type:
// integers and numbers digits, booleans true/false, anything else any
// non-empty segment.
func templateMatches(template string, item *v3.PathItem, method, reqPath string) bool {
	types := map[string]string{}
	collect := func(params []*v3.Parameter) {
		for _, p := range params {
			if p == nil || p.Schema == nil {
				continue
			}
			if sch := p.Schema.Schema(); sch != nil {
				for _, t := range sch.Type {
					if t != "null" {
						types[p.Name] = t
						break
					}
				}
			}
		}
	}
	collect(item.Parameters)
	if op := operation(item, method); op != nil {
		collect(op.Parameters)
	}
	var re strings.Builder
	re.WriteString("^")
	last := 0
	for _, m := range templateParam.FindAllStringSubmatchIndex(template, -1) {
		re.WriteString(regexp.QuoteMeta(template[last:m[0]]))
		switch types[template[m[2]:m[3]]] {
		case "integer":
			re.WriteString(`-?[0-9]+`)
		case "number":
			re.WriteString(`-?[0-9]+(?:\.[0-9]+)?`)
		case "boolean":
			re.WriteString(`(?:true|false)`)
		default:
			re.WriteString(`[^/]+`)
		}
		last = m[1]
	}
	re.WriteString(regexp.QuoteMeta(template[last:]))
	re.WriteString("$")
	ok, err := regexp.MatchString(re.String(), reqPath)
	return err == nil && ok
}
