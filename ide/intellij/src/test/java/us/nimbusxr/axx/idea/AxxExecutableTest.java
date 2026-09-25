// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.NullSource;
import org.junit.jupiter.params.provider.ValueSource;

import java.io.File;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;
import java.util.function.Function;

@DisplayName("AxxExecutable")
class AxxExecutableTest {
    @TempDir Path project;

    private final List<String> lookedUp = new ArrayList<>();

    /** A PATH lookup that records the names it is asked for and answers {@code found}. */
    private Function<String, Path> path(Path found) {
        return name -> {
            lookedUp.add(name);
            return found;
        };
    }

    private Path file(String relative) throws IOException {
        Path file = project.resolve(relative);
        Files.createDirectories(file.getParent());
        return Files.writeString(file, "");
    }

    private Path executable(String relative) throws IOException {
        Path file = file(relative);
        assertTrue(file.toFile().setExecutable(true));
        return file;
    }

    private String pathOf(String... dirs) {
        return String.join(
                File.pathSeparator,
                Arrays.stream(dirs).map(dir -> project.resolve(dir).toString()).toList());
    }

    private Path resolve(String setting, Function<String, Path> onPath, Path... installed) {
        return AxxExecutable.resolve(setting, project, onPath, List.of(installed));
    }

    @ParameterizedTest(name = "\"{0}\"")
    @DisplayName("looks up axx on PATH by default")
    @NullSource
    @ValueSource(strings = {"", "  ", "axx", " axx "})
    void defaultsToAxxOnPath(String setting) {
        Path onPath = Path.of("/usr/local/bin/axx");
        assertEquals(onPath, resolve(setting, path(onPath)));
        assertEquals(List.of("axx"), lookedUp);
    }

    @Test
    @DisplayName("looks up another command name on PATH")
    void looksUpCommandName() {
        Path onPath = Path.of("/opt/bin/axx-dev");
        assertEquals(onPath, resolve("axx-dev", path(onPath)));
        assertEquals(List.of("axx-dev"), lookedUp);
    }

    @Test
    @DisplayName("prefers axx on PATH to the install locations")
    void prefersPath() throws IOException {
        Path onPath = Path.of("/somewhere/axx");
        assertEquals(onPath, resolve("axx", path(onPath), executable("go/bin/axx")));
    }

    @Test
    @DisplayName("tries the install locations in order when axx is not on PATH")
    void triesInstallLocations() throws IOException {
        Path missing = project.resolve("gobin/axx");
        Path notExecutable = file("go/bin/axx");
        assertTrue(notExecutable.toFile().setExecutable(false));
        Path local = executable(".local/bin/axx");
        Path homebrew = executable("homebrew/bin/axx");
        Path expected = Files.isExecutable(notExecutable) ? notExecutable : local;
        assertEquals(expected, resolve("axx", path(null), missing, notExecutable, local, homebrew));
    }

    @Test
    @DisplayName("tries the install locations only for the default command")
    void installLocationsOnlyForDefault() throws IOException {
        Path installed = executable("go/bin/axx");
        assertNull(resolve("axx-dev", path(null), installed));
        assertNull(resolve("bin/axx", path(null), installed));
    }

    @Test
    @DisplayName("finds nothing when axx is neither on PATH nor installed")
    void notInstalled() {
        assertNull(resolve("axx", path(null), project.resolve("go/bin/axx")));
    }

    @Test
    @DisplayName("resolves a relative path against the project directory")
    void resolvesRelativePath() throws IOException {
        Path binary = file("bin/axx");
        assertEquals(binary, resolve("bin/axx", path(null)));
        assertTrue(lookedUp.isEmpty());
    }

    @Test
    @DisplayName("uses an absolute path as is")
    void usesAbsolutePath() throws IOException {
        Path binary = file("axx");
        Path elsewhere = Path.of("elsewhere");
        assertEquals(
                binary, AxxExecutable.resolve(binary.toString(), elsewhere, path(null), List.of()));
        assertTrue(lookedUp.isEmpty());
    }

    @Test
    @DisplayName("finds nothing when the path does not exist")
    void missingPath() {
        assertNull(resolve("bin/axx", path(Path.of("/usr/local/bin/axx"))));
    }

    @Test
    @DisplayName("lists go install, the install script and Homebrew locations")
    void installLocations() {
        Path home = Path.of("/home/me");
        assertEquals(
                List.of(
                        Path.of("/home/me/go/bin/axx"),
                        Path.of("/home/me/.local/bin/axx"),
                        Path.of("/opt/homebrew/bin/axx"),
                        Path.of("/usr/local/bin/axx")),
                AxxExecutable.installLocations(name -> null, home, false));
    }

    @Test
    @DisplayName("puts $GOBIN and the first $GOPATH entry first")
    void goEnvironment() {
        Path home = Path.of("/home/me");
        Map<String, String> env =
                Map.of(
                        "GOBIN", "/opt/gobin",
                        "GOPATH", "/work/go" + File.pathSeparator + "/other/go");
        assertEquals(
                List.of(
                        Path.of("/opt/gobin/axx"),
                        Path.of("/work/go/bin/axx"),
                        Path.of("/home/me/.local/bin/axx"),
                        Path.of("/opt/homebrew/bin/axx"),
                        Path.of("/usr/local/bin/axx")),
                AxxExecutable.installLocations(env::get, home, false));
    }

    @Test
    @DisplayName("lists each location once")
    void noDuplicates() {
        Path home = Path.of("/home/me");
        Map<String, String> env = Map.of("GOBIN", "/home/me/go/bin");
        List<Path> locations = AxxExecutable.installLocations(env::get, home, false);
        assertEquals(Path.of("/home/me/go/bin/axx"), locations.get(0));
        assertEquals(locations.size(), locations.stream().distinct().count());
        assertEquals(4, locations.size());
    }

    @Test
    @DisplayName("looks for axx.exe on Windows, where the install script uses LOCALAPPDATA")
    void windows() {
        Path home = Path.of("/Users/me");
        Map<String, String> env = Map.of("LOCALAPPDATA", "/Users/me/AppData/Local");
        assertEquals(
                List.of(
                        Path.of("/Users/me/go/bin/axx.exe"),
                        Path.of("/Users/me/AppData/Local/axx/bin/axx.exe"),
                        Path.of("/Users/me/.local/bin/axx.exe")),
                AxxExecutable.installLocations(env::get, home, true));
    }

    @Test
    @DisplayName("finds a command in the first PATH directory that has it")
    void findsOnPath() throws IOException {
        Files.createDirectories(project.resolve("first"));
        Path binary = executable("second/axx");
        executable("third/axx");
        String path = pathOf("first", "second", "third");
        assertEquals(binary, AxxExecutable.findOnPath("axx", path, List.of()));
    }

    @Test
    @DisplayName("skips PATH entries that are not executable files")
    void skipsNonExecutables() throws IOException {
        Files.createDirectories(project.resolve("dir/axx"));
        Path plain = file("plain/axx");
        assertTrue(plain.toFile().setExecutable(false));
        Path binary = executable("bin/axx");
        // Where files cannot lose the executable permission (Windows), only the directory is
        // skipped.
        String path =
                Files.isExecutable(plain)
                        ? pathOf("dir", "bin")
                        : pathOf("dir", "plain", "bin");
        assertEquals(binary, AxxExecutable.findOnPath("axx", path, List.of()));
    }

    @Test
    @DisplayName("tries the Windows executable extensions first")
    void triesExtensions() throws IOException {
        Path exe = executable("bin/axx.exe");
        executable("bin/axx");
        String path = pathOf("bin");
        assertEquals(exe, AxxExecutable.findOnPath("axx", path, List.of(".com", ".exe")));
    }

    @Test
    @DisplayName("finds nothing when no PATH directory has the command")
    void notOnPath() {
        assertNull(AxxExecutable.findOnPath("axx", pathOf("."), List.of()));
        assertNull(AxxExecutable.findOnPath("axx", "", List.of()));
        assertNull(AxxExecutable.findOnPath("axx", null, List.of()));
    }

    @Test
    @DisplayName("explains what was not found, and where it looked")
    void notFoundMessage() {
        Path home = Path.of("/home/me");
        List<Path> installed = AxxExecutable.installLocations(name -> null, home, false);
        String message = AxxExecutable.notFoundMessage("run axx", null, installed, home);
        String sep = File.separator;
        String goBin = "~" + sep + "go" + sep + "bin";
        String expected = "'axx' command is not on PATH, nor in " + goBin + ", ";
        assertTrue(message.contains(expected), message);
        assertTrue(message.contains(Path.of("/usr/local/bin").toString()), message);
        assertTrue(message.startsWith("Cannot run axx: the 'axx' command"), message);
        assertTrue(message.endsWith("set the axx executable in Settings | Tools | axx."), message);
        assertTrue(
                AxxExecutable.notFoundMessage("run axx", "axx-dev", installed, home)
                        .contains("the 'axx-dev' command is not on PATH."));
        assertTrue(
                AxxExecutable.notFoundMessage("run axx", "bin/axx", installed, home)
                        .contains("'bin/axx' does not exist"));
    }
}
