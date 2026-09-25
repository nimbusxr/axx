package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Image names the tasks build on (task Dockerfiles and compose files refer
// to them by these names).
const (
	baseImage     = "axx-evals-base:latest"
	verifierImage = "axx-evals-verifier:latest"
	addressImage  = "axx-evals-address-service:latest"
)

func cmdImages(args []string) error {
	fs := flag.NewFlagSet("images", flag.ExitOnError)
	root := fs.String("root", "", "the evals directory")
	noCache := fs.Bool("no-cache", false, "build without Docker's cache")
	version := fs.String("axx-version", "", "version stamped into the axx binary (default: git describe)")
	_ = fs.Parse(args)
	dir, err := evalsRoot(*root)
	if err != nil {
		return err
	}
	return buildImages(dir, *noCache, *version)
}

func buildImages(evalsDir string, noCache bool, version string) error {
	repo := filepath.Dir(evalsDir)
	if version == "" {
		version = gitDescribe(repo)
	}
	extra := []string{}
	if noCache {
		extra = append(extra, "--no-cache")
	}
	dockerfile := filepath.Join(evalsDir, "images", "base", "Dockerfile")
	for _, target := range []struct{ name, tag string }{{"base", baseImage}, {"verifier", verifierImage}} {
		args := append([]string{
			"build", "-f", dockerfile, "--target", target.name, "-t", target.tag,
			"--build-arg", "AXX_VERSION=" + version,
		}, extra...)
		args = append(args, repo)
		if err := runStreaming("docker", args...); err != nil {
			return fmt.Errorf("building %s: %w", target.tag, err)
		}
	}
	args := append([]string{"build", "-f", filepath.Join(evalsDir, "images", "address-service", "Dockerfile"), "-t", addressImage}, extra...)
	args = append(args, filepath.Join(evalsDir, "app", "wiremock"))
	if err := runStreaming("docker", args...); err != nil {
		return fmt.Errorf("building %s: %w", addressImage, err)
	}
	fmt.Printf("built %s, %s and %s (axx %s)\n", baseImage, verifierImage, addressImage, version)
	return nil
}

func gitDescribe(repo string) string {
	out, err := exec.CommandContext(context.Background(), "git", "-C", repo, "describe", "--tags", "--always", "--dirty").Output()
	if err != nil {
		return "0.0.0-evals"
	}
	v := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	if v == "" {
		return "0.0.0-evals"
	}
	if v[0] < '0' || v[0] > '9' {
		v = "0.0.0-evals+" + v
	}
	return v
}

func runStreaming(name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func cmdPacks(args []string) error {
	fs := flag.NewFlagSet("packs", flag.ExitOnError)
	_ = fs.Parse(args)
	packs, version, err := imagePacks()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(packs))
	for p := range packs {
		names = append(names, p)
	}
	sort.Strings(names)
	fmt.Printf("axx %s: %s\n", version, strings.Join(names, ", "))
	return nil
}

// imagePacks lists the step packs the common starting project has in the
// evals image, and the image's axx version.
func imagePacks() (map[string]bool, string, error) {
	out, err := exec.CommandContext(context.Background(), "docker", "run", "--rm", "-w", "/opt/evals/workspace", "--entrypoint", "axx", baseImage, "steps", "--json").Output()
	if err != nil {
		return nil, "", fmt.Errorf("listing the packs in %s (run `evals images` first): %w", baseImage, err)
	}
	var env struct {
		Data struct {
			Steps []struct {
				Pack string `json:"pack"`
			} `json:"steps"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, "", err
	}
	packs := map[string]bool{"core": true}
	for _, s := range env.Data.Steps {
		packs[s.Pack] = true
	}
	vout, _ := exec.CommandContext(context.Background(), "docker", "run", "--rm", "--entrypoint", "axx", baseImage, "version", "--json").Output()
	var venv struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	_ = json.Unmarshal(bytes.TrimSpace(vout), &venv)
	return packs, venv.Data.Version, nil
}
