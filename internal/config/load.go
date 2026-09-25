package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/interp"
)

// FileNames are the config file names searched for, in order.
var FileNames = []string{"axx.yaml", "axx.yml"}

// LoadOptions controls config discovery and overlays.
type LoadOptions struct {
	// Path is an explicit config file (--config). Empty means search upward
	// from WorkDir.
	Path    string
	WorkDir string
	// Profile selects profiles.<name> and axx.<name>.yaml overlays.
	Profile string
	// Properties override `properties` (from -D name=value).
	Properties map[string]string
	// LookupEnv resolves ${env:..}; defaults to os.LookupEnv.
	LookupEnv func(string) (string, bool)
}

// Error codes produced while loading configuration.
const (
	CodeNotFound = "AXX-E0100"
	CodeSyntax   = "AXX-E0101"
	CodeInvalid  = "AXX-E0102"
	CodeProfile  = "AXX-E0104"
)

// Load finds, merges, interpolates and validates the configuration. A
// missing axx.yaml is not an error: axx runs with defaults.
func Load(opts LoadOptions) (*Config, error) {
	if opts.WorkDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		opts.WorkDir = wd
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}
	if opts.Profile == "" {
		opts.Profile, _ = opts.LookupEnv("AXX_PROFILE")
	}

	file, err := discover(opts)
	if err != nil {
		return nil, err
	}

	tree := yaml.MapSlice{}
	var sources []source
	dir := opts.WorkDir
	if file != "" {
		dir = filepath.Dir(file)
		t, src, err := readYAML(file)
		if err != nil {
			return nil, err
		}
		tree = t
		sources = append(sources, src)
	}

	// Overlays: profiles.<p>, axx.<p>.yaml, axx.local.yaml.
	if opts.Profile != "" {
		prof, ok := lookup(tree, "profiles", opts.Profile)
		profFile := filepath.Join(dir, "axx."+opts.Profile+".yaml")
		_, statErr := os.Stat(profFile)
		if !ok && statErr != nil {
			return nil, axxerr.New(CodeProfile, exitcode.Usage, "profile %q not found", opts.Profile).
				WithHint("define profiles.%s in axx.yaml or create axx.%s.yaml", opts.Profile, opts.Profile)
		}
		if ok {
			if m, isMap := prof.(yaml.MapSlice); isMap {
				tree = merge(tree, m)
			}
		}
		if statErr == nil {
			t, src, err := readYAML(profFile)
			if err != nil {
				return nil, err
			}
			tree = merge(tree, t)
			sources = append(sources, src)
		}
	}
	if local := filepath.Join(dir, "axx.local.yaml"); fileExists(local) {
		t, src, err := readYAML(local)
		if err != nil {
			return nil, err
		}
		tree = merge(tree, t)
		sources = append(sources, src)
	}

	// Properties: file values, overridden by -D, expanded against env and
	// themselves (one level).
	props := map[string]string{}
	if p, ok := lookup(tree, "properties"); ok {
		if m, isMap := p.(yaml.MapSlice); isMap {
			for _, it := range m {
				props[fmt.Sprint(it.Key)] = scalarString(it.Value)
			}
		}
	}
	for k, v := range opts.Properties {
		props[k] = v
	}
	envOnly := &interp.Resolver{Lookups: map[string]interp.Lookup{"env": opts.LookupEnv}}
	for k, v := range props {
		props[k] = envOnly.MustExpand(v)
	}
	resolver := NewResolver(props, opts.LookupEnv)
	tree = setKey(tree, "properties", mapSliceFrom(props))
	tree = interpolateTree(tree, resolver).(yaml.MapSlice)

	jsonDoc, err := toJSON(tree)
	if err != nil {
		return nil, err
	}
	if err := validate(jsonDoc, sources); err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := strictUnmarshal(jsonDoc, cfg); err != nil {
		return nil, axxerr.Wrap(err, CodeInvalid, exitcode.Usage, "invalid configuration").At(displayFile(file), 0, 0)
	}
	cfg.Dir = dir
	cfg.File = file
	cfg.Properties = props
	cfg.sources = sources
	return cfg, nil
}

// NewResolver builds the interpolation resolver for env and sys lookups.
func NewResolver(props map[string]string, lookupEnv func(string) (string, bool)) *interp.Resolver {
	return &interp.Resolver{Lookups: map[string]interp.Lookup{
		"env": lookupEnv,
		"sys": func(name string) (string, bool) {
			if v, ok := props[name]; ok {
				return v, true
			}
			return builtinSys(name)
		},
	}}
}

// builtinSys provides the JVM system properties feature files commonly used.
func builtinSys(name string) (string, bool) {
	switch name {
	case "user.dir":
		wd, err := os.Getwd()
		return wd, err == nil
	case "user.home":
		h, err := os.UserHomeDir()
		return h, err == nil
	case "os.name":
		return osName(), true
	case "file.separator":
		return string(filepath.Separator), true
	}
	return "", false
}

func discover(opts LoadOptions) (string, error) {
	if opts.Path != "" {
		p := opts.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(opts.WorkDir, p)
		}
		if !fileExists(p) {
			return "", axxerr.New(CodeNotFound, exitcode.Usage, "config file %s not found", opts.Path)
		}
		return p, nil
	}
	for dir := opts.WorkDir; ; {
		for _, n := range FileNames {
			if p := filepath.Join(dir, n); fileExists(p) {
				return p, nil
			}
		}
		// Stop at the repository root.
		if fileExists(filepath.Join(dir, ".git")) {
			return "", nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

type source struct {
	file string
	data []byte
}

func readYAML(file string) (yaml.MapSlice, source, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, source{}, axxerr.Wrap(err, CodeNotFound, exitcode.Usage, "cannot read %s", displayFile(file))
	}
	src := source{file: file, data: data}
	if len(bytes.TrimSpace(data)) == 0 {
		return yaml.MapSlice{}, src, nil
	}
	var tree any
	if err := yaml.UnmarshalWithOptions(data, &tree, yaml.UseOrderedMap()); err != nil {
		return nil, src, axxerr.New(CodeSyntax, exitcode.Usage, "%s: invalid YAML\n%s", displayFile(file), yaml.FormatError(err, false, true))
	}
	m, ok := tree.(yaml.MapSlice)
	if !ok {
		if tree == nil {
			return yaml.MapSlice{}, src, nil
		}
		return nil, src, axxerr.New(CodeInvalid, exitcode.Usage, "%s: top level must be a mapping", displayFile(file))
	}
	return m, src, nil
}

func lookup(tree yaml.MapSlice, keys ...string) (any, bool) {
	var cur any = tree
	for _, k := range keys {
		m, ok := cur.(yaml.MapSlice)
		if !ok {
			return nil, false
		}
		found := false
		for _, it := range m {
			if fmt.Sprint(it.Key) == k {
				cur, found = it.Value, true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return cur, true
}

func setKey(tree yaml.MapSlice, key string, val any) yaml.MapSlice {
	for i, it := range tree {
		if fmt.Sprint(it.Key) == key {
			tree[i].Value = val
			return tree
		}
	}
	if m, ok := val.(yaml.MapSlice); ok && len(m) == 0 {
		return tree
	}
	return append(tree, yaml.MapItem{Key: key, Value: val})
}

func mapSliceFrom(m map[string]string) yaml.MapSlice {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(yaml.MapSlice, 0, len(keys))
	for _, k := range keys {
		out = append(out, yaml.MapItem{Key: k, Value: m[k]})
	}
	return out
}

// merge deep-merges overlay into base: mappings merge, everything else
// (including lists) is replaced.
func merge(base, overlay yaml.MapSlice) yaml.MapSlice {
	out := append(yaml.MapSlice{}, base...)
	for _, it := range overlay {
		key := fmt.Sprint(it.Key)
		idx := -1
		for i, b := range out {
			if fmt.Sprint(b.Key) == key {
				idx = i
				break
			}
		}
		if idx < 0 {
			out = append(out, it)
			continue
		}
		bm, bok := out[idx].Value.(yaml.MapSlice)
		om, ook := it.Value.(yaml.MapSlice)
		if bok && ook {
			out[idx].Value = merge(bm, om)
		} else {
			out[idx].Value = it.Value
		}
	}
	return out
}

func interpolateTree(v any, r *interp.Resolver) any {
	switch t := v.(type) {
	case yaml.MapSlice:
		out := make(yaml.MapSlice, len(t))
		for i, it := range t {
			if fmt.Sprint(it.Key) == "profiles" {
				out[i] = it // overlays are interpolated after merging, never here
				continue
			}
			out[i] = yaml.MapItem{Key: it.Key, Value: interpolateTree(it.Value, r)}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = interpolateTree(e, r)
		}
		return out
	case string:
		return r.MustExpand(t)
	default:
		return v
	}
}

func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// toJSON converts an ordered YAML tree into JSON, preserving key order.
func toJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeJSON(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeJSON(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case yaml.MapSlice:
		b.WriteByte('{')
		for i, it := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			k, _ := json.Marshal(fmt.Sprint(it.Key))
			b.Write(k)
			b.WriteByte(':')
			if err := writeJSON(b, it.Value); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeJSON(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case uint64:
		b.WriteString(strconv.FormatUint(t, 10))
	default:
		enc, err := json.Marshal(t)
		if err != nil {
			return err
		}
		b.Write(enc)
	}
	return nil
}

// positionOf returns the line/column of a JSON-pointer-like path in the
// first source that contains it.
func positionOf(sources []source, path []string) (file string, line, col int) {
	for i := len(sources) - 1; i >= 0; i-- {
		src := sources[i]
		f, err := parser.ParseBytes(src.data, 0)
		if err != nil {
			continue
		}
		for n := len(path); n >= 0; n-- {
			pb := (&yaml.PathBuilder{}).Root()
			for _, seg := range path[:n] {
				if idx, err := strconv.Atoi(seg); err == nil {
					pb = pb.Index(uint(idx))
				} else {
					pb = pb.Child(seg)
				}
			}
			node, err := pb.Build().FilterFile(f)
			if err == nil && node != nil {
				tok := node.GetToken()
				if tok != nil && tok.Position != nil {
					return src.file, tok.Position.Line, tok.Position.Column
				}
			}
		}
	}
	return "", 0, 0
}

func displayFile(file string) string {
	if file == "" {
		return "axx.yaml"
	}
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, file); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return file
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() || err == nil && filepath.Base(p) == ".git"
}

var errNoSchema = errors.New("config schema unavailable")

// ProjectDir returns the directory of the axx.yaml that Load would use for
// path (as given to --config) from the working directory, or the working
// directory itself when there is none.
func ProjectDir(path string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	file, err := discover(LoadOptions{Path: path, WorkDir: wd})
	if err != nil {
		return "", err
	}
	if file == "" {
		return wd, nil
	}
	return filepath.Dir(file), nil
}
