// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.nio.file.Path;
import java.util.List;
import java.util.Set;

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
                        List.of("features/x.feature:7"),
                        List.of("--tags", "@smoke"),
                        null,
                        null,
                        null));
    }

    @Test
    @DisplayName("adds --debug-steps when debugging, unless the arguments choose a port")
    void debugSteps() {
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--debug-steps=2345",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.pauseOnFailure=true",
                        "features"),
                RunTargets.runArguments(List.of("features"), List.of(), 2345, null, List.of()));
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.pauseOnFailure=true",
                        "features",
                        "--debug-steps=4000"),
                RunTargets.runArguments(
                        List.of("features"), List.of("--debug-steps=4000"), 2345, null, List.of()));
    }

    @Test
    @DisplayName("watches the browsers: one worker, watch, slowdown, and no pausing")
    void watch() {
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.watch=true",
                        "--set",
                        "packs.web-core.slowdown=300ms",
                        "features/shop-portal.feature:12",
                        "--tags",
                        "@web"),
                RunTargets.runArguments(
                        List.of("features/shop-portal.feature:12"),
                        List.of("--tags", "@web"),
                        null,
                        300,
                        null));
    }

    @Test
    @DisplayName("a slowdown of 0 adds none")
    void noSlowdown() {
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.watch=true",
                        "features"),
                RunTargets.runArguments(List.of("features"), List.of(), null, 0, null));
    }

    @Test
    @DisplayName("debugs: one worker, pause on failure, and the steps to pause before")
    void debug() {
        List<String> steps =
                List.of("features/shop-portal.feature:9", "features/shop-portal.feature:14");
        // Step code breakpoints still stop.
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--debug-steps=2345",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.pauseOnFailure=true",
                        "--pause-at",
                        "features/shop-portal.feature:9",
                        "--pause-at",
                        "features/shop-portal.feature:14",
                        "features"),
                RunTargets.runArguments(List.of("features"), List.of(), 2345, null, steps));
        // Without a Go debugger or breakpoints, a failed scenario still pauses.
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.pauseOnFailure=true",
                        "features/shop-portal.feature:12"),
                RunTargets.runArguments(
                        List.of("features/shop-portal.feature:12"),
                        List.of(),
                        null,
                        null,
                        List.of()));
    }

    @Test
    @DisplayName("debugs while watching the browsers with one worker")
    void debugWhileWatching() {
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.watch=true",
                        "--set",
                        "packs.web-core.slowdown=300ms",
                        "--set",
                        "packs.web-core.pauseOnFailure=true",
                        "--pause-at",
                        "features/shop-portal.feature:9",
                        "features"),
                RunTargets.runArguments(
                        List.of("features"),
                        List.of(),
                        null,
                        300,
                        List.of("features/shop-portal.feature:9")));
    }

    @Test
    @DisplayName("pauses before the steps in the files the run reaches, in order")
    void pauseAtReachedSteps() {
        Path portal = SUITE.resolve("features/web/shop-portal.feature");
        Path tracking = SUITE.resolve("features/tracking.feature");
        Path other = Path.of("/work/other-suite/features/tracking.feature");
        List<RunTargets.Step> steps =
                List.of(
                        new RunTargets.Step(portal, 24),
                        new RunTargets.Step(tracking, 9),
                        new RunTargets.Step(portal, 9),
                        new RunTargets.Step(portal, 24),
                        new RunTargets.Step(other, 5));

        // A scenario of a file: the steps of that file, whichever scenario they are in.
        assertEquals(
                List.of(
                        "features/web/shop-portal.feature:9",
                        "features/web/shop-portal.feature:24"),
                RunTargets.pauseAt(steps, List.of("features/web/shop-portal.feature:20"), SUITE));
        // A directory: the files in it.
        assertEquals(
                List.of(
                        "features/tracking.feature:9",
                        "features/web/shop-portal.feature:9",
                        "features/web/shop-portal.feature:24"),
                RunTargets.pauseAt(steps, List.of("features"), SUITE));
        assertEquals(
                List.of(), RunTargets.pauseAt(steps, List.of("features/returns.feature"), SUITE));
        // The whole suite: the files of the working directory, not another suite's.
        assertEquals(
                List.of(
                        "features/tracking.feature:9",
                        "features/web/shop-portal.feature:9",
                        "features/web/shop-portal.feature:24"),
                RunTargets.pauseAt(steps, List.of(), SUITE));
        // A file outside the working directory stays absolute, as its target does.
        assertEquals(
                List.of(other + ":5"),
                RunTargets.pauseAt(steps, List.of(other.toString()), SUITE));
    }

    @Test
    @DisplayName("finds the scenario a file the web pack names belongs to")
    void findLocation() {
        Set<String> locations =
                Set.of(
                        "file:///work/acceptance/features/shop-portal.feature:1",
                        "file:///work/acceptance/features/shop-portal.feature:12",
                        "file:///work/acceptance/features/shop-portal.feature:25",
                        "file:///work/acceptance/web/features/shop-portal.feature:12");
        assertEquals(
                "file:///work/acceptance/features/shop-portal.feature:12",
                RunTargets.findLocation(locations, SUITE, "features/shop-portal.feature", 12));
        // axx ran below the project directory: the only location that ends with the file.
        assertEquals(
                "file:///work/acceptance/features/shop-portal.feature:25",
                RunTargets.findLocation(
                        locations, SUITE.resolve("features"), "features/shop-portal.feature", 25));
        assertEquals(
                "file:///work/acceptance/web/features/shop-portal.feature:12",
                RunTargets.findLocation(
                        locations, null, "web/features/shop-portal.feature", 12));
        assertEquals(
                "file:///work/acceptance/features/shop-portal.feature:12",
                RunTargets.findLocation(
                        locations, null, "/work/acceptance/features/shop-portal.feature", 12));
        // Two files end the same way, and neither is in the working directory.
        assertNull(
                RunTargets.findLocation(
                        locations, Path.of("/other"), "features/shop-portal.feature", 12));
        assertNull(RunTargets.findLocation(locations, SUITE, "features/shop-portal.feature", 7));
        assertNull(RunTargets.findLocation(locations, SUITE, "features/other.feature", 12));
    }
}
