package mirror

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGitHub(t *testing.T) {
	const path = "/appium/WebDriverAgent/releases/download/v1.0.0/a.zip"
	t.Setenv(EnvGitHub, "")
	if got := GitHub(path); got != "https://github.com"+path {
		t.Errorf("without a mirror: %s", got)
	}
	t.Setenv(EnvGitHub, " https://repo.example.com/artifactory/github/ ")
	if got := GitHub(path); got != "https://repo.example.com/artifactory/github"+path {
		t.Errorf("with a mirror: %s", got)
	}
}

func TestNuGetPackage(t *testing.T) {
	const index = `{"version":"3.0.0","resources":[
		{"@id":"https://repo.example.com/nuget/query","@type":"SearchQueryService"},
		{"@id":"https://repo.example.com/nuget/flat/","@type":"PackageBaseAddress/3.0.0"}]}`
	var asked []string
	get := func(_ context.Context, addr string) ([]byte, error) {
		asked = append(asked, addr)
		return []byte(index), nil
	}
	t.Setenv(EnvNuGet, "")
	got, err := NuGetPackage(context.Background(), get, "Microsoft.Windows.CppWinRT", "2.0.250303.1")
	if err != nil || asked[0] != "https://api.nuget.org/v3/index.json" {
		t.Fatalf("nuget.org's index: %v %v", asked, err)
	}
	if want := "https://repo.example.com/nuget/flat/microsoft.windows.cppwinrt/2.0.250303.1/microsoft.windows.cppwinrt.2.0.250303.1.nupkg"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	// A feed that needs credentials lists its addresses without them: the package's address
	// takes the index's.
	t.Setenv(EnvNuGet, "https://ci:secret@repo.example.com/nuget/index.json")
	if got, err = NuGetPackage(context.Background(), get, "A", "1.0.0"); err != nil || !strings.HasPrefix(got, "https://ci:secret@repo.example.com/nuget/flat/a/1.0.0/") {
		t.Errorf("credentials: %s %v", got, err)
	}

	bad := func(_ context.Context, _ string) ([]byte, error) { return []byte(`{"resources":[]}`), nil }
	if _, err := NuGetPackage(context.Background(), bad, "A", "1.0.0"); err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "PackageBaseAddress") {
		t.Errorf("a feed without packages: %v", err)
	}
}

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"https://repo.example.com/go":               "https://repo.example.com/go",
		"https://ci:secret@repo.example.com/go":     "https://xxxxx@repo.example.com/go",
		"https://secret@repo.example.com/go":        "https://xxxxx@repo.example.com/go",
		"https://ci:%2Fsecret@repo.example.com/go/": "https://xxxxx@repo.example.com/go/",
		// Not an address, still credentials.
		"https://ci:secret@repo.example.com/%zz": "https://xxxxx@repo.example.com/%zz",
		"repo.example.com/go":                    "repo.example.com/go",
	} {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%s) = %s, want %s", in, got, want)
		}
	}
	// net/http hides a password, never a user's name, which can be a token too.
	err := RedactErr(&url.Error{Op: "Get", URL: "https://secret@repo.example.com/go", Err: errors.New("refused")})
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("RedactErr: %v", err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://ci:secret@127.0.0.1:1/go", nil)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
	if err == nil || strings.Contains(RedactErr(err).Error(), "ci:") {
		t.Errorf("a failed request's error: %v", err)
	}
}

func TestHelp(t *testing.T) {
	t.Setenv(EnvGitHub, "")
	if got := GitHubHelp(); !strings.Contains(got, "cannot reach github.com, set AXX_GITHUB_URL") {
		t.Errorf("without a mirror: %s", got)
	}
	t.Setenv(EnvNuGet, "https://ci:secret@repo.example.com/nuget/index.json")
	if got := NuGetHelp(); strings.Contains(got, "secret") || !strings.Contains(got, "AXX_NUGET_URL names, https://xxxxx@repo.example.com/nuget/index.json") {
		t.Errorf("with a mirror: %s", got)
	}
}
