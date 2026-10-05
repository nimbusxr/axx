// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import static org.junit.jupiter.api.Assertions.assertEquals;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

@DisplayName("AxxProfiles")
class AxxProfilesTest {
    @TempDir Path dir;

    private void write(String name, String content) throws IOException {
        Files.writeString(dir.resolve(name), content);
    }

    @Test
    @DisplayName("finds the profiles of axx.yaml in order, then the axx.<name>.yaml files")
    void findsProfiles() throws IOException {
        write(
                "axx.yaml",
                String.join(
                        "\n",
                        "version: 1",
                        "profiles:",
                        "  # one per platform",
                        "  web:",
                        "    run: {uses: [web-core]}",
                        "",
                        "  ios:",
                        "    run:",
                        "      uses: [mobile-ios]",
                        "  \"android\": {run: {uses: [mobile-android]}}",
                        "services:",
                        "  parcels:",
                        "    command: ./parcels"));
        write("axx.watch.yaml", "run: {watch: true}\n");
        write("axx.ci.yaml", "run: {workers: 4}\n");
        write("axx.ios.yaml", "run: {workers: 1}\n");
        write("axx.local.yaml", "run: {workers: 2}\n");
        write("axx-packs.yaml", "packs: [rest]\n");
        assertEquals(List.of("web", "ios", "android", "ci", "watch"), AxxProfiles.find(dir));
    }

    @Test
    @DisplayName("reads a one-line profiles mapping and axx.yml")
    void flowAndYml() throws IOException {
        write("axx.yml", "profiles: {web: {run: {uses: [web-core]}}, ios: {}}  # platforms\n");
        assertEquals(List.of("web", "ios"), AxxProfiles.find(dir));
    }

    @Test
    @DisplayName("has none without profiles")
    void none() throws IOException {
        write("axx.yaml", "version: 1\nrun:\n  paths: [features]\n");
        assertEquals(List.of(), AxxProfiles.find(dir));
        assertEquals(List.of(), AxxProfiles.find(dir.resolve("missing")));
    }

    @Test
    @DisplayName("stores profiles as --profile takes them")
    void joinAndParse() {
        assertEquals("ios,watch", AxxProfiles.join(List.of("ios", "watch")));
        assertEquals(List.of("ios", "watch"), AxxProfiles.parse(" ios, watch,,"));
        assertEquals(List.of(), AxxProfiles.parse(""));
    }
}
