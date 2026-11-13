// Package selfupdate finds axx's releases on GitHub, says what changed between
// two of them, and replaces the axx that runs with another, checked against the
// checksums its release publishes, as the install scripts do.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/mod/semver"

	"github.com/nimbusxr/axx/internal/mirror"
	"github.com/nimbusxr/axx/internal/version"
)

// Repo is axx's repository.
const Repo = "nimbusxr/axx"

// Nightly is the tag of the rolling build of main.
const Nightly = "nightly"

// ChecksumsFile is the file of a release that lists its archives' checksums.
const ChecksumsFile = "checksums.txt"

const (
	maxAPIBytes       = 32 << 20
	maxChecksumBytes  = 1 << 20
	maxArchiveBytes   = 1 << 30
	apiTimeout        = 30 * time.Second
	downloadTimeout   = 10 * time.Minute
	binaryPermissions = 0o755
)

var (
	// ErrNotFound means a release or one of its files does not exist.
	ErrNotFound = errors.New("not found")
	// ErrChecksum means a download is not what its release published.
	ErrChecksum = errors.New("checksum mismatch")
	// ErrNotListed means a release's checksums list no such file: the
	// release does not have it.
	ErrNotListed = errors.New("not in the release")
)

// EnvDownloadURL names a mirror of the releases' files, for a network that cannot
// reach GitHub: <url>/v0.5.0/checksums.txt, and the archives beside it.
const EnvDownloadURL = "AXX_DOWNLOAD_URL"

// Client reads axx's releases. GitHub is all it talks to, or the mirror
// EnvDownloadURL names: Token, for the API's rate limit, goes to the API and to
// nothing else.
type Client struct {
	API       string // the repository's API, without a trailing slash
	Downloads string // where releases' files are, without a trailing slash
	Token     string
	HTTP      *http.Client
}

// New is a Client of GitHub, with GITHUB_TOKEN if it is set.
func New() *Client {
	c := &Client{
		API:       "https://api.github.com/repos/" + Repo,
		Downloads: "https://github.com/" + Repo + "/releases/download",
		Token:     os.Getenv("GITHUB_TOKEN"),
		HTTP:      &http.Client{Timeout: downloadTimeout},
	}
	if mirror := os.Getenv(EnvDownloadURL); mirror != "" {
		c.Downloads = strings.TrimRight(mirror, "/")
	}
	return c
}

// Release is a release of axx, as GitHub's API gives it.
type Release struct {
	Tag       string    `json:"tag_name"`
	Draft     bool      `json:"draft"`
	Published time.Time `json:"published_at"`
	Body      string    `json:"body"`
	Assets    []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

// Version is the release's version, without the tag's v.
func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

func (r Release) has(name string) bool {
	for _, a := range r.Assets {
		if a.Name == name {
			return true
		}
	}
	return false
}

// cliTag matches the CLI's tags: the WireMock extension and the IntelliJ plugin
// have releases of their own (wiremock-openapi-v*, intellij-v*), and nightly.
var cliTag = regexp.MustCompile(`^v\d`)

// Releases lists the CLI's published releases, newest first. Every 0.x release
// is a GitHub pre-release, so it lists them rather than ask for the latest,
// which skips those.
func (c *Client) Releases(ctx context.Context) ([]Release, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	b, err := c.get(ctx, c.API+"/releases?per_page=100", c.Token, maxAPIBytes)
	if err != nil {
		return nil, err
	}
	var all []Release
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, fmt.Errorf("reading GitHub's list of releases: %w", err)
	}
	var out []Release
	for _, r := range all {
		if !r.Draft && cliTag.MatchString(r.Tag) {
			out = append(out, r)
		}
	}
	return out, nil
}

// Newest is the newest release with its files up for a platform: a release is
// published before its build uploads them, so the newest one can have none yet.
func Newest(releases []Release, goos, goarch string) *Release {
	for i := range releases {
		r := &releases[i]
		if r.has(ChecksumsFile) && r.has(ArchiveName(r.Version(), goos, goarch)) {
			return r
		}
	}
	return nil
}

// Find is the release of a version, if the list has it.
func Find(releases []Release, v string) *Release {
	for i := range releases {
		if releases[i].Version() == v {
			return &releases[i]
		}
	}
	return nil
}

// Compare orders two versions: negative when a is older than b.
func Compare(a, b string) int {
	return semver.Compare("v"+strings.TrimPrefix(a, "v"), "v"+strings.TrimPrefix(b, "v"))
}

// Between are the releases after from, up to and including to, newest first.
func Between(releases []Release, from, to string) []Release {
	var out []Release
	for _, r := range releases {
		if Compare(r.Version(), from) > 0 && Compare(r.Version(), to) <= 0 {
			out = append(out, r)
		}
	}
	return out
}

// ArchiveName is the file of a release that holds a platform's axx.
func ArchiveName(v, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "axx_" + v + "_" + goos + "_" + goarch + ext
}

// Section is a part of a release's notes.
type Section struct {
	Title string   `json:"title"`
	Items []string `json:"items"`
}

var (
	noteLink  = regexp.MustCompile(`\s*\(\[[^\]]*\]\([^)]*\)\)`)
	noteScope = regexp.MustCompile(`^\*\*([^*]+)\*\*`)
)

// Notes reads a release's notes, which release-please writes, without their
// links: what a terminal shows.
func Notes(body string) []Section {
	var out []Section
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, " \r")
		switch {
		case strings.HasPrefix(line, "### "):
			title := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "### "), "⚠"))
			if strings.EqualFold(title, "BREAKING CHANGES") {
				title = "Breaking changes"
			}
			out = append(out, Section{Title: title})
		case strings.HasPrefix(line, "* ") && len(out) > 0:
			item := noteLink.ReplaceAllString(strings.TrimPrefix(line, "* "), "")
			item = noteScope.ReplaceAllString(item, "$1")
			out[len(out)-1].Items = append(out[len(out)-1].Items, item)
		}
	}
	return out
}

// DocsArchiveName is the file of a release that holds its documentation site,
// the site's files at its top (index.html, 404.html, _astro/...).
func DocsArchiveName(v string) string {
	return "axx_" + strings.TrimPrefix(v, "v") + "_docs.tar.gz"
}

// FetchDocs downloads the archive of a release's documentation site into dir
// and checks it against the release's checksums. A release from before axx
// published its docs has none: ErrNotListed.
func (c *Client) FetchDocs(ctx context.Context, tag, dir string) (string, error) {
	return c.fetchListed(ctx, tag, dir, func(string) (string, error) { return DocsArchiveName(tag), nil })
}

// Fetched is a release's axx, checked and unpacked.
type Fetched struct {
	Binary  string // the axx it holds, executable
	Archive string // the archive it came from
}

// Fetch downloads a platform's archive of a release into dir, checks it against
// the release's checksums and unpacks axx there.
func (c *Client) Fetch(ctx context.Context, tag, goos, goarch, dir string) (*Fetched, error) {
	archive, err := c.fetchListed(ctx, tag, dir, func(sums string) (string, error) {
		if tag != Nightly {
			return ArchiveName(strings.TrimPrefix(tag, "v"), goos, goarch), nil
		}
		// The nightly's archives are named after its build: take the one its checksums list.
		if name := nightlyArchive(sums, goos, goarch); name != "" {
			return name, nil
		}
		return "", fmt.Errorf("the nightly build for %s/%s: %w", goos, goarch, ErrNotFound)
	})
	if err != nil {
		return nil, err
	}
	name := filepath.Base(archive)
	binary := filepath.Join(dir, BinaryName(goos))
	if err := unpack(archive, BinaryName(goos), binary); err != nil {
		return nil, fmt.Errorf("unpacking %s: %w", name, err)
	}
	return &Fetched{Binary: binary, Archive: archive}, nil
}

// fetchListed downloads the file of a release that name picks from its
// checksums into dir, and checks it against them.
func (c *Client) fetchListed(ctx context.Context, tag, dir string, name func(sums string) (string, error)) (string, error) {
	base := c.Downloads + "/" + tag + "/"
	sums, err := c.get(ctx, base+ChecksumsFile, "", maxChecksumBytes)
	if err != nil {
		return "", fmt.Errorf("%s of %s: %w", ChecksumsFile, tag, err)
	}
	file, err := name(string(sums))
	if err != nil {
		return "", err
	}
	want := checksumOf(string(sums), file)
	if want == "" {
		return "", fmt.Errorf("%s is not listed in %s: %w", file, ChecksumsFile, ErrNotListed)
	}
	path := filepath.Join(dir, file)
	got, err := c.download(ctx, base+file, path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", file, err)
	}
	if got != want {
		return "", fmt.Errorf("%w for %s: %s says %s, the download is %s", ErrChecksum, file, ChecksumsFile, want, got)
	}
	return path, nil
}

// BinaryName is axx's file name on a platform.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "axx.exe"
	}
	return "axx"
}

func checksumOf(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == name {
			return f[0]
		}
	}
	return ""
}

func nightlyArchive(sums, goos, goarch string) string {
	suffix := "_" + goos + "_" + goarch + ".tar.gz"
	if goos == "windows" {
		suffix = "_" + goos + "_" + goarch + ".zip"
	}
	for _, line := range strings.Split(sums, "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.HasPrefix(f[1], "axx_") && strings.HasSuffix(f[1], suffix) {
			return f[1]
		}
	}
	return ""
}

func (c *Client) request(ctx context.Context, url, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "axx/"+version.Get().Version)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// A mirror's address can carry credentials, which messages leave out.
		return nil, mirror.RedactErr(err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	return resp, nil
}

func (c *Client) get(ctx context.Context, url, token string, limit int64) ([]byte, error) {
	resp, err := c.request(ctx, url, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// download writes a URL to a file, and gives its SHA-256.
func (c *Client) download(ctx context.Context, url, path string) (string, error) {
	resp, err := c.request(ctx, url, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArchiveBytes)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), f.Close()
}

func unpack(archive, member, dst string) error {
	if strings.HasSuffix(archive, ".zip") {
		return unpackZip(archive, member, dst)
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("it holds no %s", member)
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && hdr.Name == member {
			return writeBinary(tr, dst)
		}
	}
}

func unpackZip(archive, member, dst string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == member {
			r, err := f.Open()
			if err != nil {
				return err
			}
			defer r.Close()
			return writeBinary(r, dst)
		}
	}
	return fmt.Errorf("it holds no %s", member)
}

func writeBinary(r io.Reader, dst string) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, binaryPermissions)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(r, maxArchiveBytes)); err != nil {
		return err
	}
	return f.Close()
}

// moveWait is how long Move waits for a file something else has open.
var moveWait = 30 * time.Second

// errorSharingViolation is Windows's answer for a file that something has open and
// won't let be moved.
const errorSharingViolation = syscall.Errno(32)

// Move is os.Rename for an axx. On Windows, something else having a file open, as a
// scanner does for a moment with a program it has just seen, keeps it from moving:
// that is no failure, so Move waits it out.
func Move(from, to string) error {
	err := os.Rename(from, to)
	if runtime.GOOS != "windows" {
		return err
	}
	for wait, deadline := 50*time.Millisecond, time.Now().Add(moveWait); errors.Is(err, errorSharingViolation) && time.Now().Before(deadline); wait = min(2*wait, time.Second) {
		time.Sleep(wait)
		err = os.Rename(from, to)
	}
	return err
}

// Replace puts fresh, a file in exe's directory, in exe's place, in one step: a
// failure leaves exe as it was. Windows cannot replace the running file, but can
// rename it: the old one goes when it can, here or at the next upgrade.
func Replace(exe, fresh string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(fresh, exe)
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := Move(exe, old); err != nil {
		return err
	}
	if err := Move(fresh, exe); err != nil {
		return errors.Join(err, Move(old, exe))
	}
	_ = os.Remove(old)
	return nil
}

// Method is how an axx gets upgraded.
type Method string

const (
	// Archive is the archive of a release, from the install scripts or by
	// hand: axx replaces itself.
	Archive Method = "release archive"
	// Homebrew's axx is Homebrew's to replace.
	Homebrew Method = "homebrew"
	// GoInstall is `go install`'s axx, which has no archive to come from.
	GoInstall Method = "go install"
	// Source is a build of a checkout, which upgrades with the checkout.
	Source Method = "source"
	// Prerelease is a nightly or a snapshot: nothing says what is newer.
	Prerelease Method = "prerelease"
)

// homebrewDirs are where Homebrew keeps what it installs, a directory per version.
var homebrewDirs = []string{"/Caskroom/", "/Cellar/"}

// homebrewPrefix is the path up to Homebrew's directories, if exe is in them.
func homebrewPrefix(exe string) (string, bool) {
	exe = filepath.ToSlash(exe)
	for _, dir := range homebrewDirs {
		if i := strings.Index(exe, dir); i >= 0 {
			return filepath.FromSlash(exe[:i]), true
		}
	}
	return "", false
}

// HomebrewLink is the link in Homebrew's bin to the axx at a path in its directories,
// and empty for any other path. It outlives the version at that path, whose directory
// an upgrade removes.
func HomebrewLink(exe string) string {
	prefix, ok := homebrewPrefix(exe)
	if !ok {
		return ""
	}
	return filepath.Join(prefix, "bin", filepath.Base(exe))
}

// Detect says how the axx at a path upgrades, from the version stamped in its
// build (empty when `go` built it) and the version it reports.
func Detect(exe, stamped, current string) Method {
	_, brewed := homebrewPrefix(exe)
	switch {
	case brewed:
		return Homebrew
	case stamped == "" && version.FromModuleProxy(current) && current != "0.0.0-dev":
		return GoInstall
	case stamped == "":
		return Source
	case !version.FromModuleProxy(current):
		return Prerelease
	default:
		return Archive
	}
}
