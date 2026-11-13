// Package mirror says where axx and its packs download other projects' files from: their own
// sites, or the mirrors an organization names, such as the remote repositories of an artifact
// repository manager. It also keeps the credentials a mirror's address may carry
// (https://user:token@host/) out of messages.
package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// EnvGitHub names a mirror of github.com, for the files axx downloads from other projects'
// releases there (Appium's drivers, Piper): <mirror>/<owner>/<repository>/releases/download/...
// axx's own releases have a setting of their own, AXX_DOWNLOAD_URL.
const EnvGitHub = "AXX_GITHUB_URL"

// EnvNuGet names a NuGet feed by its service index, as nuget.config names one
// (https://api.nuget.org/v3/index.json is nuget.org's), for the NuGet packages axx downloads.
const EnvNuGet = "AXX_NUGET_URL"

// EnvMaven names a Maven repository (a mirror of Maven Central), for the
// Java tools axx downloads from Maven Central: JaCoCo's agent and CLI.
const EnvMaven = "AXX_MAVEN_URL"

const (
	gitHub = "https://github.com"
	nuGet  = "https://api.nuget.org/v3/index.json"
	maven  = "https://repo1.maven.org/maven2"
)

// Maven is the address of path in Maven Central, or in the repository
// AXX_MAVEN_URL names: path is a file's, like
// /org/jacoco/org.jacoco.agent/0.8.16/org.jacoco.agent-0.8.16-runtime.jar.
func Maven(path string) string {
	return setting(EnvMaven, maven) + path
}

// MavenHelp says how a machine that cannot reach Maven Central gets its
// files, for a failed download's message.
func MavenHelp() string {
	return help(EnvMaven, "Maven Central (repo1.maven.org)")
}

// GitHub is the address of path on github.com, or on the mirror AXX_GITHUB_URL names. path is
// what follows github.com, like /appium/WebDriverAgent/releases/download/v9.1.0/a.zip.
func GitHub(path string) string {
	return setting(EnvGitHub, gitHub) + path
}

// GitHubHelp says how a machine that cannot reach github.com gets its files,
// for a failed download's message.
func GitHubHelp() string {
	return help(EnvGitHub, "github.com")
}

// NuGetHelp says how a machine that cannot reach nuget.org gets its
// packages, for a failed download's message.
func NuGetHelp() string {
	return help(EnvNuGet, "nuget.org")
}

func help(env, site string) string {
	if v := os.Getenv(env); strings.TrimSpace(v) != "" {
		return fmt.Sprintf("the download came from the mirror %s names, %s", env, Redact(strings.TrimSpace(v)))
	}
	return fmt.Sprintf("if this machine cannot reach %s, set %s to a mirror of it", site, env)
}

// setting is the address an environment variable names, without a trailing slash, or def.
func setting(env, def string) string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv(env)), "/"); v != "" {
		return v
	}
	return def
}

// NuGetPackage is the address of a NuGet package's .nupkg on nuget.org, or on the feed
// AXX_NUGET_URL names: the feed's service index says where its packages are (its
// PackageBaseAddress). get downloads an address.
func NuGetPackage(ctx context.Context, get func(context.Context, string) ([]byte, error), id, version string) (string, error) {
	index := setting(EnvNuGet, nuGet)
	b, err := get(ctx, index)
	if err != nil {
		return "", err
	}
	var doc struct {
		Resources []struct {
			ID   string `json:"@id"`
			Type string `json:"@type"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return "", fmt.Errorf("%s is not a NuGet feed's service index: %w", Redact(index), err)
	}
	for _, r := range doc.Resources {
		if r.Type != "PackageBaseAddress/3.0.0" {
			continue
		}
		id, version := strings.ToLower(id), strings.ToLower(version)
		return withUser(strings.TrimRight(r.ID, "/")+"/"+id+"/"+version+"/"+id+"."+version+".nupkg", index), nil
	}
	return "", fmt.Errorf("the NuGet feed %s does not say where its packages are (no PackageBaseAddress/3.0.0)", Redact(index))
}

// withUser gives addr the credentials from's address carries, when both are on one host and
// addr has none: a feed's index lists its own addresses without the credentials that read it.
func withUser(addr, from string) string {
	a, err := url.Parse(addr)
	f, ferr := url.Parse(from)
	if err != nil || ferr != nil || f.User == nil || a.User != nil || a.Host != f.Host {
		return addr
	}
	a.User = f.User
	return a.String()
}

// Redact is an address without the credentials it may carry, for messages: a token can be the
// user's name as well as the password.
func Redact(addr string) string {
	u, err := url.Parse(addr)
	if err == nil {
		if u.User == nil {
			return addr
		}
		u.User = url.User("xxxxx")
		return u.String()
	}
	// No address, but maybe credentials: what comes before the last @ of
	// its host.
	scheme, rest, ok := strings.Cut(addr, "://")
	if !ok {
		return addr
	}
	host, _, _ := strings.Cut(rest, "/")
	if i := strings.LastIndex(host, "@"); i >= 0 {
		return scheme + "://xxxxx@" + rest[i+1:]
	}
	return addr
}

// RedactErr is err with the address of a failed request redacted, as net/http's errors name it
// (it hides only a password).
func RedactErr(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		ue.URL = Redact(ue.URL)
	}
	return err
}
