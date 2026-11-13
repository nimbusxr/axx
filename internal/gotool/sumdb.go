package gotool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/sumdb/note"

	"github.com/nimbusxr/axx/internal/mirror"
)

// knownSumDBs are the checksum databases the go command knows by name, and
// their keys.
var knownSumDBs = map[string]string{
	"sum.golang.org": "sum.golang.org+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8",
}

// checkToolchain checks a downloaded golang.org/toolchain against Go's
// checksum database, as the go command checks every Go release it
// downloads: always, whatever GONOSUMDB or GOPRIVATE say, as no go.sum can
// list one.
func checkToolchain(ctx context.Context, archive, vers, cacheDir string) error {
	db, err := dialSumDB(ctx, cacheDir)
	if err != nil {
		return err
	}
	lines, err := sumdb.NewClient(db).Lookup(toolchainModule, vers)
	if msg := db.securityError(); msg != "" {
		return fmt.Errorf("the checksum database %s: %s", db.name, msg)
	}
	if err != nil {
		return fmt.Errorf("the checksum database %s: %w", db.name, err)
	}
	var want string
	for _, line := range lines {
		if s, ok := strings.CutPrefix(line, toolchainModule+" "+vers+" "); ok {
			want = s
		}
	}
	if want == "" {
		return fmt.Errorf("the checksum database %s does not list %s@%s", db.name, toolchainModule, vers)
	}
	got, err := dirhash.HashZip(archive, dirhash.Hash1)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the %s@%s downloaded is not the one Go's checksum database %s lists: its hash is %s, the database's %s", toolchainModule, vers, db.name, got, want)
	}
	return nil
}

// sumDB reaches a checksum database for sumdb.Client, as the go command
// does: through the first proxy in GOPROXY that serves it, else directly, or
// at the address GOSUMDB names. It keeps the latest signed tree it saw in
// axx's cache, so a database that later forks its history is caught.
type sumDB struct {
	ctx       context.Context
	key, name string
	base      string // GOSUMDB's address, or found by findBase
	proxies   []proxy
	dir       string

	baseOnce sync.Once
	baseErr  error
	mu       sync.Mutex
	security string
}

func dialSumDB(ctx context.Context, cacheDir string) (*sumDB, error) {
	gosumdb := goSetting("GOSUMDB")
	switch gosumdb {
	case "":
		gosumdb = "sum.golang.org"
	case "off":
		return nil, errors.New("GOSUMDB=off: the go command checks every Go release it downloads against Go's checksum database, and so does axx. Let GOSUMDB name the database, or a mirror of it (GOSUMDB=\"sum.golang.org https://<mirror>\")")
	case "sum.golang.google.cn":
		gosumdb = "sum.golang.org https://sum.golang.google.cn"
	}
	f := strings.Fields(gosumdb)
	if len(f) == 0 || len(f) > 2 {
		return nil, fmt.Errorf("GOSUMDB=%q: name a checksum database, and then maybe its address", gosumdb)
	}
	if k := knownSumDBs[f[0]]; k != "" {
		f[0] = k
	}
	verifier, err := note.NewVerifier(f[0])
	if err != nil {
		return nil, fmt.Errorf("GOSUMDB=%s: %w", gosumdb, err)
	}
	db := &sumDB{ctx: ctx, key: f[0], name: verifier.Name(), dir: filepath.Join(cacheDir, "sumdb")}
	if u, err := url.Parse("https://" + db.name); err != nil || u.Host == "" || strings.HasSuffix(db.name, "/") {
		return nil, fmt.Errorf("GOSUMDB names a database %q, which is no host[/path]", db.name)
	}
	if len(f) == 2 {
		if _, err := url.Parse(f[1]); err != nil {
			return nil, fmt.Errorf("GOSUMDB's address %s is not an address", mirror.Redact(f[1]))
		}
		db.base = strings.TrimRight(f[1], "/")
		return db, nil
	}
	if db.proxies, err = proxies(); err != nil {
		return nil, err
	}
	return db, nil
}

// findBase finds the database's address: the first proxy that says it
// serves it (<proxy>/sumdb/<name>/supported), else the database itself. A
// proxy that does not (404, 410) moves on to the next, as one that fails does
// when | follows it; "direct" and "off" end the search.
func (db *sumDB) findBase() {
	if db.base != "" {
		return
	}
	for _, p := range db.proxies {
		if p.url == "direct" || p.url == "off" {
			break
		}
		_, err := get(db.ctx, p.url+"/sumdb/"+db.name+"/supported", 1<<10)
		if err == nil {
			db.base = p.url + "/sumdb/" + db.name
			return
		}
		if !errors.Is(err, errNotFound) && !p.fallBackOnError {
			db.baseErr = err
			return
		}
	}
	db.base = "https://" + db.name
}

func (db *sumDB) ReadRemote(path string) ([]byte, error) {
	db.baseOnce.Do(db.findBase)
	if db.baseErr != nil {
		return nil, db.baseErr
	}
	return get(db.ctx, db.base+path, 10<<20)
}

func (db *sumDB) ReadConfig(file string) ([]byte, error) {
	if file == "key" {
		return []byte(db.key), nil
	}
	data, err := os.ReadFile(filepath.Join(db.dir, filepath.FromSlash(file)))
	if errors.Is(err, fs.ErrNotExist) {
		// No tree seen yet: start from the empty one.
		return []byte{}, nil
	}
	return data, err
}

func (db *sumDB) WriteConfig(file string, old, new []byte) error {
	if file == "key" {
		return errors.New("the key is not written")
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	p := filepath.Join(db.dir, filepath.FromSlash(file))
	cur, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(cur) > 0 && !bytes.Equal(cur, old) {
		return sumdb.ErrWriteConflict
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".latest-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(new)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), p)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// ReadCache keeps no tiles: axx looks up one record per Go release.
func (db *sumDB) ReadCache(string) ([]byte, error) { return nil, fs.ErrNotExist }

func (db *sumDB) WriteCache(string, []byte) {}

func (db *sumDB) Log(string) {}

func (db *sumDB) SecurityError(msg string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.security = msg
}

func (db *sumDB) securityError() string {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.security
}
