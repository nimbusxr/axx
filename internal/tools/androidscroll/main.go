// Command androidscroll builds axx's scroll service for Android devices (mobile-android), the
// APK a release publishes beside its archives, in its checksums (release.yml's android-scroll
// job), and that release's axx downloads.
//
//	go run ./internal/tools/androidscroll -out axx_0.7.0_android-scroll.apk -version 0.7.0 -keystore release.jks -alias axx
//
// It needs the Android SDK (ANDROID_HOME: its build-tools and a platform) and a JDK. The
// keystore's password is read from ANDROID_SCROLL_KEYSTORE_PASSWORD, never from the command
// line. With no keystore, it signs with a key it makes for this build: for a dry run, and for
// a release cut before NimbusXR's key was a secret of the repository. Android asks that an
// app's updates have the key of the one installed, and axx installs the service afresh where
// the keys differ, so such a release works; it says so, loudly.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	mobileandroid "github.com/nimbusxr/axx/packs/mobile/android"
)

func main() {
	out := flag.String("out", "", "the APK to write")
	version := flag.String("version", "", "the service's version: the release's, like 0.7.0")
	keystore := flag.String("keystore", "", "the keystore that signs it; none: a key made for this build")
	alias := flag.String("alias", "axx", "the key's alias in the keystore")
	flag.Parse()
	if err := run(context.Background(), *out, *version, *keystore, *alias); err != nil {
		fmt.Fprintln(os.Stderr, "androidscroll:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, out, version, keystore, alias string) error {
	if out == "" || version == "" {
		return fmt.Errorf("-out and -version are needed")
	}
	sdk := os.Getenv("ANDROID_HOME")
	if sdk == "" {
		sdk = os.Getenv("ANDROID_SDK_ROOT")
	}
	if sdk == "" {
		return fmt.Errorf("the Android SDK is not found: set ANDROID_HOME to it")
	}
	password := os.Getenv("ANDROID_SCROLL_KEYSTORE_PASSWORD")
	if keystore == "" {
		dir, err := os.MkdirTemp("", "axx-scroll-key-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		random := make([]byte, 24)
		if _, err := rand.Read(random); err != nil {
			return err
		}
		keystore, alias, password = filepath.Join(dir, "build.jks"), "axx", hex.EncodeToString(random)
		if err := mobileandroid.MakeScrollKeystore(ctx, keystore, alias, password, "axx scroll service, a key of one build"); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "androidscroll: WARNING: no keystore was given, so the service is signed with a key made for this build, not NimbusXR's")
	} else if password == "" {
		return fmt.Errorf("the keystore's password is not set: ANDROID_SCROLL_KEYSTORE_PASSWORD")
	}
	if err := mobileandroid.BuildScrollService(ctx, mobileandroid.ScrollBuild{SDK: sdk, Out: out, Version: version, Keystore: keystore, Alias: alias, Password: password}); err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
