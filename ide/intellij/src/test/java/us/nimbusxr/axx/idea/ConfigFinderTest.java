// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

@DisplayName("ConfigFinder")
class ConfigFinderTest {
    @TempDir Path tmp;

    private Path project;

    private Path project() throws IOException {
        if (project == null) {
            // A repository root, so the upward search stops here.
            project = Files.createDirectories(tmp.resolve("repo"));
            Files.createDirectories(project.resolve(".git"));
        }
        return project;
    }

    private Path touch(Path dir, String relative) throws IOException {
        Path file = dir.resolve(relative);
        Files.createDirectories(file.getParent());
        return Files.writeString(file, "");
    }

    @ParameterizedTest(name = "{0}")
    @DisplayName("finds a config in the directory itself")
    @ValueSource(strings = {"axx.yaml", "axx.yml"})
    void findsConfigInDirectory(String name) throws IOException {
        Path config = touch(project(), name);
        assertEquals(config, ConfigFinder.find(project()));
    }

    @Test
    @DisplayName("prefers axx.yaml over axx.yml")
    void prefersYaml() throws IOException {
        touch(project(), "axx.yml");
        Path config = touch(project(), "axx.yaml");
        assertEquals(config, ConfigFinder.find(project()));
    }

    @Test
    @DisplayName("finds a config in a subdirectory")
    void findsConfigInSubdirectory() throws IOException {
        Path config = touch(project(), "acceptance/axx.yaml");
        assertEquals(config, ConfigFinder.find(project()));
    }

    @Test
    @DisplayName("searches three levels down, no deeper")
    void searchesThreeLevels() throws IOException {
        touch(project(), "a/b/c/d/axx.yaml");
        assertNull(ConfigFinder.find(project()));
        Path config = touch(project(), "a/b/c/axx.yaml");
        assertEquals(config, ConfigFinder.find(project()));
    }

    @Test
    @DisplayName("prefers the shallowest config")
    void prefersShallowest() throws IOException {
        touch(project(), "a/b/axx.yaml");
        Path config = touch(project(), "z/axx.yaml");
        assertEquals(config, ConfigFinder.find(project()));
    }

    @ParameterizedTest(name = "{0}")
    @DisplayName("skips hidden, dependency and build output directories")
    @ValueSource(
            strings = {".cache", "node_modules", "vendor", "build", "dist", "target", "out", "bin"})
    void skipsDirectories(String dir) throws IOException {
        touch(project(), dir + "/axx.yaml");
        assertNull(ConfigFinder.find(project()));
    }

    @Test
    @DisplayName("finds a config upward from the directory")
    void findsConfigUpward() throws IOException {
        Path config = touch(project(), "axx.yaml");
        Path features = Files.createDirectories(project().resolve("features/orders"));
        assertEquals(config, ConfigFinder.find(features));
    }

    @Test
    @DisplayName("stops the upward search at the repository root")
    void stopsAtRepositoryRoot() throws IOException {
        touch(tmp, "axx.yaml");
        assertNull(ConfigFinder.find(project()));
    }

    @Test
    @DisplayName("finds the nearest config above a directory, not below it")
    void nearestAbove() throws IOException {
        Path root = touch(project(), "axx.yaml");
        Path suite = touch(project(), "acceptance/axx.yaml");
        Path features = Files.createDirectories(project().resolve("acceptance/features/orders"));
        assertEquals(suite, ConfigFinder.nearestAbove(features));
        assertEquals(suite, ConfigFinder.nearestAbove(project().resolve("acceptance")));
        assertEquals(root, ConfigFinder.nearestAbove(project()));
        Files.delete(root);
        assertNull(ConfigFinder.nearestAbove(project()));
    }

    @Test
    @DisplayName("finds nothing without a config")
    void findsNothing() throws IOException {
        touch(project(), "features/orders.feature");
        assertNull(ConfigFinder.find(project()));
    }
}
