package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// SuiteOptions configures a Suite. Hosts construct suites; packs use them.
type SuiteOptions struct {
	// PackConfig holds each pack's axx.yaml section as JSON, keyed by pack name.
	PackConfig map[string]json.RawMessage
	// Interpolate expands ${env:..}/${sys:..} references in configuration
	// values supplied through steps (service URLs, credentials...).
	Interpolate func(string) string
	// ResolvePath resolves a resource path used by a step (seed files,
	// schemas, payload files) against the configured resource roots.
	ResolvePath func(string) (string, error)
	Logger      *slog.Logger
	// ProjectDir is the directory of axx.yaml; packs keep their run state
	// under ProjectDir/.axx.
	ProjectDir string
}

// Suite is shared by all scenarios of a run: configuration, resolution
// helpers and a cache of suite-scoped resources (connection pools, parsed
// specifications, clients).
type Suite struct {
	opts SuiteOptions

	mu      sync.Mutex
	cache   map[string]*cacheEntry
	closers []func(context.Context) error
	closed  bool
}

type cacheEntry struct {
	once sync.Once
	val  any
	err  error
}

// NewSuite creates a suite.
func NewSuite(opts SuiteOptions) *Suite {
	if opts.Interpolate == nil {
		opts.Interpolate = func(s string) string { return s }
	}
	if opts.ResolvePath == nil {
		opts.ResolvePath = func(p string) (string, error) { return p, nil }
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Suite{opts: opts, cache: map[string]*cacheEntry{}}
}

// PackConfig decodes the named pack's configuration section into v. It is a
// no-op if the section is absent.
func (s *Suite) PackConfig(pack string, v any) error {
	raw, ok := s.opts.PackConfig[pack]
	if !ok || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("packs.%s: %w", pack, err)
	}
	return nil
}

// Interpolate expands ${env:NAME}, ${sys:name} and defaults (${env:X:-y}).
func (s *Suite) Interpolate(v string) string { return s.opts.Interpolate(v) }

// ResolvePath resolves a resource path referenced from a feature file.
func (s *Suite) ResolvePath(p string) (string, error) { return s.opts.ResolvePath(p) }

// Logger returns the suite logger.
func (s *Suite) Logger() *slog.Logger { return s.opts.Logger }

// ProjectDir returns the directory of axx.yaml ("" when there is none).
func (s *Suite) ProjectDir() string { return s.opts.ProjectDir }

// OnClose registers fn to run when the suite ends (reverse order).
func (s *Suite) OnClose(fn func(context.Context) error) {
	s.mu.Lock()
	s.closers = append(s.closers, fn)
	s.mu.Unlock()
}

// Close releases suite resources. Hosts call it once at the end of a run.
func (s *Suite) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	closers := s.closers
	s.closers = nil
	s.mu.Unlock()
	var errs []error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Cached returns the suite-scoped value for key, creating it once with init.
// Concurrent callers for the same key wait for the single initialization.
// Failed initializations are cached too, so a broken resource fails fast.
func Cached[T any](s *Suite, key string, init func() (T, error)) (T, error) {
	s.mu.Lock()
	e, ok := s.cache[key]
	if !ok {
		e = &cacheEntry{}
		s.cache[key] = e
	}
	s.mu.Unlock()
	e.once.Do(func() { e.val, e.err = init() })
	if e.err != nil {
		var zero T
		return zero, e.err
	}
	return e.val.(T), nil
}
