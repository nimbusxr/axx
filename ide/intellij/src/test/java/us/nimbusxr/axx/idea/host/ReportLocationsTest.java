// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertSame;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

/** The locations of a sandboxed run's report, moved to the project the IDE has open. */
@DisplayName("ReportLocations")
class ReportLocationsTest {
    private static final String SCENARIO =
            "##teamcity[testSuiteStarted nodeId='129' parentNodeId='feature-1' name='Scenario: the"
                + " service is healthy'"
                + " locationHint='file:///sandbox/parcels/features/health.feature:6']";

    @Test
    @DisplayName("moves locations from the run's directory to the project's")
    void moves() {
        assertEquals(
                "##teamcity[testSuiteStarted nodeId='129' parentNodeId='feature-1' name='Scenario:"
                        + " the service is healthy'"
                        + " locationHint='file:///work/parcels/features/health.feature:6']",
                ReportLocations.relocate(SCENARIO, "/sandbox/parcels", "/work/parcels"));
    }

    @Test
    @DisplayName("leaves the line alone when the directories are the same or unknown")
    void same() {
        assertSame(
                SCENARIO,
                ReportLocations.relocate(SCENARIO, "/sandbox/parcels", "/sandbox/parcels/"));
        assertSame(SCENARIO, ReportLocations.relocate(SCENARIO, "", "/work/parcels"));
        assertSame(SCENARIO, ReportLocations.relocate(SCENARIO, null, "/work/parcels"));
        assertSame(SCENARIO, ReportLocations.relocate(SCENARIO, "/elsewhere", "/work/parcels"));
    }

    @Test
    @DisplayName("moves only the directory itself, not one that starts with its name")
    void wholeDirectory() {
        String line =
                "##teamcity[testStdOut nodeId='1' out='see file:///sandbox/parcels-api/x.feature:1"
                    + " and file:///sandbox/parcels/y.feature:2 and file:///sandbox/parcels:3']";
        assertEquals(
                "##teamcity[testStdOut nodeId='1' out='see file:///sandbox/parcels-api/x.feature:1"
                        + " and file:///work/parcels/y.feature:2 and file:///work/parcels:3']",
                ReportLocations.relocate(line, "/sandbox/parcels/", "/work/parcels"));
    }

    @Test
    @DisplayName("matches directories as TeamCity escapes them")
    void escaped() {
        String line =
                "##teamcity[testStarted nodeId='1' name='Given x'"
                        + " locationHint='file:///sandbox/it||s |[mine|]/features/x.feature:3']";
        assertEquals(
                "##teamcity[testStarted nodeId='1' name='Given x'"
                        + " locationHint='file:///work/O|'Brien/features/x.feature:3']",
                ReportLocations.relocate(line, "/sandbox/it|s [mine]", "/work/O'Brien"));
    }

    @Test
    @DisplayName("writes Windows directories as axx writes them in locations")
    void windows() {
        String line =
                "##teamcity[testStarted locationHint='file:///C:/sandbox/parcels/f.feature:1']";
        assertEquals(
                "##teamcity[testStarted locationHint='file:///D:/work/parcels/f.feature:1']",
                ReportLocations.relocate(line, "C:\\sandbox\\parcels", "D:\\work\\parcels"));
        assertEquals("/C:/a/b", ReportLocations.slashed("C:\\a\\b\\"));
        assertEquals("/", ReportLocations.slashed("/"));
    }
}
