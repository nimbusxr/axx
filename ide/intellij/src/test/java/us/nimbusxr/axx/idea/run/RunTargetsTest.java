// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.nio.file.Path;
import java.util.List;

@DisplayName("RunTargets")
class RunTargetsTest {
    private static final Path SUITE = Path.of("/work/acceptance");

    @Test
    @DisplayName("makes targets relative to the working directory")
    void relativeTargets() {
        Path feature = SUITE.resolve("features/orders.feature");
        assertEquals("features/orders.feature", RunTargets.target(feature, 0, SUITE));
        assertEquals("features/orders.feature:12", RunTargets.target(feature, 12, SUITE));
        assertEquals("features", RunTargets.target(SUITE.resolve("features"), 0, SUITE));
        assertEquals(".", RunTargets.target(SUITE, 0, SUITE));
    }

    @Test
    @DisplayName("keeps targets outside the working directory absolute")
    void absoluteTargets() {
        Path elsewhere = Path.of("/other/x.feature");
        assertEquals(elsewhere + ":3", RunTargets.target(elsewhere, 3, SUITE));
        assertEquals(elsewhere.toString(), RunTargets.target(elsewhere, 0, null));
    }

    @Test
    @DisplayName("turns test tree locations into targets")
    void fromLocationUrl() {
        assertEquals(
                "features/orders.feature:25",
                RunTargets.fromLocationUrl(
                        "file:///work/acceptance/features/orders.feature:25", SUITE));
        assertEquals(
                "features/orders.feature",
                RunTargets.fromLocationUrl(
                        "file:///work/acceptance/features/orders.feature", SUITE));
        assertNull(RunTargets.fromLocationUrl("java:test://Foo.bar", SUITE));
        assertNull(RunTargets.fromLocationUrl(null, SUITE));
    }

    @Test
    @DisplayName("builds the location axx reports for a line")
    void locationUrl() {
        assertEquals(
                "file:///work/acceptance/features/orders.feature:7",
                RunTargets.locationUrl("/work/acceptance/features/orders.feature", 7));
        assertEquals(
                "file:///C:/suite/x.feature:1", RunTargets.locationUrl("C:/suite/x.feature", 1));
    }

    @Test
    @DisplayName("splits a target into its path and line")
    void pathAndLine() {
        assertEquals("features/x.feature", RunTargets.pathOf("features/x.feature:14"));
        assertEquals(14, RunTargets.lineOf("features/x.feature:14"));
        assertEquals("features", RunTargets.pathOf("features"));
        assertEquals(0, RunTargets.lineOf("features"));
    }

    @Test
    @DisplayName("runs axx with the TeamCity format, targets, then the extra arguments")
    void runArguments() {
        assertEquals(
                List.of("run", "--format", "teamcity", "features/x.feature:7", "--tags", "@smoke"),
                RunTargets.runArguments(
                        List.of("features/x.feature:7"), List.of("--tags", "@smoke"), null));
    }

    @Test
    @DisplayName("adds --debug-steps when debugging, unless the arguments choose a port")
    void debugSteps() {
        assertEquals(
                List.of("run", "--format", "teamcity", "--debug-steps=2345", "features"),
                RunTargets.runArguments(List.of("features"), List.of(), 2345));
        assertEquals(
                List.of("run", "--format", "teamcity", "features", "--debug-steps=4000"),
                RunTargets.runArguments(List.of("features"), List.of("--debug-steps=4000"), 2345));
    }
}
