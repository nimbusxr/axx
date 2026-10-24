package modelsios

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
	mobileios "github.com/nimbusxr/axx/packs/mobile/ios"
)

//go:embed runtime/foundation.swift
var foundationSource []byte

//go:embed runtime/foundation-references.swift
var foundationReferences []byte

// Foundation Models' public provider API requires iOS 27. Swift's compiler
// emits the linkage names for our public-API references and replacements.
// dyld's public DYLD_INTERPOSE facility binds them at initial process loading.
func foundationLibrary(ctx context.Context, s *core.Suite) (string, error) {
	return core.Cached(s, Name+"/foundation-library", func() (string, error) {
		sdk, err := exec.CommandContext(ctx, "xcrun", "--sdk", "iphonesimulator", "--show-sdk-path").Output()
		if err != nil {
			return "", unsupported("Foundation Models needs Xcode 27: %v", err)
		}
		arch := mobileios.SimulatorArch(ctx)
		if arch == "amd64" {
			arch = "x86_64"
		}
		sum := sha256.New()
		sum.Write(foundationSource)
		sum.Write(foundationReferences)
		sum.Write(sdk)
		sum.Write([]byte(arch))
		cache := npm.CacheDir("models")
		dir := filepath.Join(cache, fmt.Sprintf("foundation-%x", sum.Sum(nil)[:12]))
		lib := filepath.Join(dir, "axx-foundation.dylib")
		if _, err := os.Stat(lib); err == nil {
			return lib, nil
		}
		if err = os.MkdirAll(cache, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(cache, "prepare-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		run := func(args ...string) error {
			if out, err := exec.CommandContext(ctx, "xcrun", args...).CombinedOutput(); err != nil {
				return unsupported("cannot prepare the Foundation Models adapter: %v: %s", err, out)
			}
			return nil
		}
		target := arch + "-apple-ios27.0-simulator"
		base := []string{"swiftc", "-sdk", strings.TrimSpace(string(sdk)), "-target", target, "-parse-as-library", "-module-cache-path", filepath.Join(tmp, "modules"), "-module-name", "AXXModels"}
		for name, data := range map[string][]byte{"foundation": foundationSource, "references": foundationReferences} {
			path := filepath.Join(tmp, name+".swift")
			if err = os.WriteFile(path, data, 0o644); err != nil {
				return "", err
			}
			if err = run(append(append([]string{}, base...), "-emit-ir", path, "-o", filepath.Join(tmp, name+".ll"))...); err != nil {
				return "", err
			}
		}
		a, err := os.ReadFile(filepath.Join(tmp, "foundation.ll"))
		if err != nil {
			return "", err
		}
		r, err := os.ReadFile(filepath.Join(tmp, "references.ll"))
		if err != nil {
			return "", err
		}
		interpose, err := foundationInterpose(string(a), string(r))
		if err != nil {
			return "", err
		}
		c, o := filepath.Join(tmp, "interpose.c"), filepath.Join(tmp, "interpose.o")
		if err = os.WriteFile(c, []byte(interpose), 0o644); err != nil {
			return "", err
		}
		if err = run("--sdk", "iphonesimulator", "clang", "-target", target, "-c", c, "-o", o); err != nil {
			return "", err
		}
		built := filepath.Join(tmp, "axx-foundation.dylib")
		if err = run(append(append([]string{}, base...), "-emit-library", filepath.Join(tmp, "foundation.swift"), o, "-o", built)...); err != nil {
			return "", err
		}
		if out, err := exec.CommandContext(ctx, "codesign", "--sign", "-", "--force", built).CombinedOutput(); err != nil {
			return "", unsupported("cannot sign Foundation Models adapter: %v: %s", err, out)
		}
		if err = os.Rename(tmp, dir); err != nil {
			if _, other := os.Stat(lib); other != nil {
				return "", err
			}
		}
		return lib, nil
	})
}

func foundationInterpose(adapter, refs string) (string, error) {
	symbol := regexp.MustCompile(`@"([^"\n]+)"`)
	find := func(ir, kind string, contains ...string) (string, error) {
		found := ""
		for _, line := range strings.Split(ir, "\n") {
			if !strings.HasPrefix(line, kind) {
				continue
			}
			m := symbol.FindStringSubmatch(line)
			if len(m) != 2 {
				continue
			}
			ok := true
			for _, part := range contains {
				if !strings.Contains(m[1], part) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			if found != "" {
				return "", unsupported("ambiguous public Foundation Models linkage")
			}
			found = m[1]
		}
		if found == "" {
			return "", unsupported("the SDK did not emit the public Foundation Models API %v", contains)
		}
		return found, nil
	}
	pairs := []struct {
		replacement string
		original    []string
	}{
		// Sessions of the system model (its type in their signature)...
		{"axxInstructions", []string{"LanguageModelSessionC5model5tools12instructions", "SystemcD0C_", "InstructionsVSg"}},
		{"axxText", []string{"LanguageModelSessionC5model5tools12instructions", "SystemcD0C_", "SSSg"}},
		{"axxBuilder", []string{"LanguageModelSessionC5model5tools12instructions", "SystemcD0C_", "KXE"}},
		{"axxTranscript", []string{"LanguageModelSessionC5model5tools10transcript", "SystemcD0C_"}},
		// ...and of any model (a generic parameter's: x, or q_ after the
		// builder's Failure).
		{"axxAnyInstructions", []string{"LanguageModelSessionC5model5tools12instructionsACx_", "InstructionsVSg"}},
		{"axxAnyText", []string{"LanguageModelSessionC5model5tools12instructionsACx_", "SSSg"}},
		{"axxAnyBuilder", []string{"LanguageModelSessionC5model5tools12instructionsACq__"}},
		{"axxAnyTranscript", []string{"LanguageModelSessionC5model5tools10transcriptACx_"}},
		{"axxAvailability", []string{"SystemLanguageModelC12availability"}},
		{"axxIsAvailable", []string{"SystemLanguageModelC11isAvailable"}},
		{"axxServerAvailability", []string{"PrivateCloudComputeLanguageModelC12availability"}},
		{"axxServerIsAvailable", []string{"PrivateCloudComputeLanguageModelC11isAvailable"}},
	}
	var out strings.Builder
	out.WriteString("// dyld's public interpose section: replacement, original. Simulator ABIs only.\n")
	for i, pair := range pairs {
		replacement, err := find(adapter, "define swiftcc", pair.replacement)
		if err != nil {
			return "", err
		}
		original, err := find(refs, "declare swiftcc", pair.original...)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "extern void original%d(void) __asm__(\"_%s\");\nextern void replacement%d(void) __asm__(\"_%s\");\n__attribute__((used,section(\"__DATA,__interpose\"))) static const struct { const void *replacement; const void *original; } pair%d = { replacement%d, original%d };\n", i, original, i, replacement, i, i, i)
	}
	return out.String(), nil
}
