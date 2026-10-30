package mobileandroid

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/npm"
)

// Agent is an ART TI agent for a debuggable app's process: its native library, of C++, and the
// Java classes it loads, of one source file each. BuildAgent builds it.
type Agent struct {
	// Name names the library: lib<Name>.so.
	Name string
	// Native is its C++ source; Java, its classes' source, in the file JavaFile.
	Native, Java []byte
	JavaFile     string
}

// BuildAgent builds an agent for a device of the ABI, with the SDK's NDK, build-tools and
// platform, and a JDK (its jvmti.h, javac): once, into axx's cache, where it is kept for the
// next runs. It is the folder that has the library (lib<Name>.so) and the classes
// (classes.dex). axx itself stays pure Go: what it builds runs on the device.
func BuildAgent(ctx context.Context, sdk, abi string, a Agent) (string, error) {
	triple := map[string]string{"arm64-v8a": "aarch64-linux-android26", "x86_64": "x86_64-linux-android26"}[abi]
	if triple == "" {
		return "", fmt.Errorf("an emulator of the %s ABI cannot run axx's agents: arm64-v8a and x86_64 can", abi)
	}
	host := map[string]string{"darwin": "darwin-x86_64", "linux": "linux-x86_64", "windows": "windows-x86_64"}[runtime.GOOS]
	ndk := os.Getenv("ANDROID_NDK_HOME")
	if ndk == "" {
		dirs, _ := filepath.Glob(filepath.Join(sdk, "ndk", "*"))
		slices.Sort(dirs)
		for i := len(dirs) - 1; i >= 0; i-- {
			compiler := triple + "-clang++"
			if runtime.GOOS == "windows" {
				compiler += ".cmd"
			}
			if _, err := os.Stat(filepath.Join(dirs[i], "toolchains", "llvm", "prebuilt", host, "bin", compiler)); err == nil {
				ndk = dirs[i]
				break
			}
		}
	}
	tools, _ := filepath.Glob(filepath.Join(sdk, "build-tools", "*"))
	slices.Sort(tools)
	platforms, _ := filepath.Glob(filepath.Join(sdk, "platforms", "android-*", "android.jar"))
	slices.Sort(platforms)
	javaHome := os.Getenv("JAVA_HOME")
	if javaHome == "" && runtime.GOOS == "darwin" {
		out, err := exec.CommandContext(ctx, "/usr/libexec/java_home").Output()
		if err == nil {
			javaHome = strings.TrimSpace(string(out))
		}
	}
	if javaHome == "" {
		javac, err := exec.LookPath("javac")
		if err == nil {
			real, _ := filepath.EvalSymlinks(javac)
			javaHome = filepath.Dir(filepath.Dir(real))
		}
	}
	if ndk == "" || len(tools) == 0 || len(platforms) == 0 || javaHome == "" {
		return "", errors.New("the NDK, build-tools, an Android platform or a JDK is missing")
	}
	header, err := os.ReadFile(filepath.Join(javaHome, "include", "jvmti.h"))
	if err != nil {
		return "", fmt.Errorf("cannot read the JDK's jvmti.h: %w", err)
	}
	h := sha256.New()
	for _, data := range [][]byte{[]byte(a.Name), a.Native, a.Java, header, []byte(ndk + tools[len(tools)-1] + platforms[len(platforms)-1] + javaHome + abi)} {
		h.Write(data)
	}
	cache := npm.CacheDir(cacheFor(a.Name))
	dir := filepath.Join(cache, fmt.Sprintf("android-%x", h.Sum(nil)[:12]))
	if _, err := os.Stat(filepath.Join(dir, "classes.dex")); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(cache, "prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for name, data := range map[string][]byte{"agent.cc": a.Native, a.JavaFile: a.Java, "jvmti.h": header} {
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o644); err != nil {
			return "", err
		}
	}
	classes := filepath.Join(tmp, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		return "", err
	}
	compiler := filepath.Join(ndk, "toolchains", "llvm", "prebuilt", host, "bin", triple+"-clang++")
	if runtime.GOOS == "windows" {
		compiler += ".cmd"
	}
	javac := filepath.Join(javaHome, "bin", "javac")
	d8 := filepath.Join(tools[len(tools)-1], "d8")
	if runtime.GOOS == "windows" {
		javac += ".exe"
		d8 += ".bat"
	}
	run := func(exe string, args ...string) error {
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Env = append(os.Environ(), "JAVA_HOME="+javaHome)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w: %s", filepath.Base(exe), err, out)
		}
		return nil
	}
	if err := run(compiler, "-shared", "-fPIC", "-static-libstdc++", "-std=c++17", "-O2", "-I"+tmp, filepath.Join(tmp, "agent.cc"), "-llog", "-ldl", "-o", filepath.Join(tmp, "lib"+a.Name+".so")); err != nil {
		return "", err
	}
	if err := run(javac, "--release", "8", "-classpath", platforms[len(platforms)-1], "-d", classes, filepath.Join(tmp, a.JavaFile)); err != nil {
		return "", err
	}
	args := []string{"--min-api", "26", "--lib", platforms[len(platforms)-1], "--output", tmp}
	if err := filepath.WalkDir(classes, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".class") {
			args = append(args, path)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if err := run(d8, args...); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, other := os.Stat(filepath.Join(dir, "classes.dex")); other != nil {
			return "", err
		}
	}
	return dir, nil
}

// cacheFor is the folder of axx's cache an agent is built into: models-android's where it
// always was.
func cacheFor(name string) string {
	if name == "axx-models" {
		return "models"
	}
	return "mobile"
}
