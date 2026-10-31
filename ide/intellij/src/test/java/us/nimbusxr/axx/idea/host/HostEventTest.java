// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import java.util.List;

/** The events {@code axx host --ide} writes, as it writes them. */
@DisplayName("HostEvent")
class HostEventTest {
    /** An approval as the host writes it, with its definitions shortened. */
    static final String APPROVAL =
            "{\"event\":\"approval\",\"warnings\":[\"services.api: compose service api mounts /tmp"
                    + " of this machine\"],\"run\":1,\"command\":\"axx run --compact\",\"id\":1,"
                    + "\"approved\":\"services:\\n  api:\\n    build: .\\n\","
                    + "\"proposed\":\"services:\\n  api:\\n    build: .\\n    volumes: [/tmp]\\n\","
                    + "\"diff\":\"--- approved\\n+++ proposed\\n@@ -1,3 +1,4 @@\\n services:\\n"
                    + "   api:\\n     build: .\\n+    volumes: [/tmp]\\n\"}";

    /** An approval the host asks as it starts: no run asked it. */
    static final String START_APPROVAL =
            "{\"event\":\"approval\",\"id\":1,\"approved\":\"services:\\n  api:\\n"
                    + "    build: .\\n\",\"proposed\":\"services:\\n  api:\\n    build: .\\n"
                    + "    privileged: true\\n\",\"diff\":\"+    privileged: true\\n\","
                    + "\"warnings\":[\"services.api: compose service api is privileged\"]}";

    @Test
    @DisplayName("parses serving: where the host listens, its services, sources and warnings")
    void parsesServing() {
        HostEvent serving =
                HostEvent.parse(
                        "{\"event\":\"serving\",\"addr\":\"127.0.0.1:51673\","
                                + "\"dir\":\"/work/parcels\",\"services\":[\"api\"],"
                                + "\"sources\":[\"compose.yaml\"],\"warnings\":[\"services.api:"
                                + " compose service api mounts /tmp of this machine\"],"
                                + "\"changed\":true}");
        assertNotNull(serving);
        assertEquals(HostEvent.SERVING, serving.event());
        assertEquals("127.0.0.1:51673", serving.addr());
        assertEquals("/work/parcels", serving.dir());
        assertEquals(List.of("api"), serving.services());
        assertEquals(List.of("compose.yaml"), serving.sources());
        assertEquals(
                List.of("services.api: compose service api mounts /tmp of this machine"),
                serving.warnings());
        assertTrue(serving.changed());
    }

    @Test
    @DisplayName("leaves what an event lacks empty")
    void absentFields() {
        HostEvent serving =
                HostEvent.parse(
                        "{\"event\":\"serving\",\"addr\":\"127.0.0.1:1\",\"dir\":\"/work/parcels\","
                                + "\"services\":[\"api\"]}");
        assertNotNull(serving);
        assertEquals(List.of(), serving.sources());
        assertEquals(List.of(), serving.warnings());
        assertFalse(serving.changed());
        assertEquals("", serving.ide());
        assertNull(serving.error());
        assertNull(serving.result());

        // An empty report line has no line at all.
        HostEvent empty = HostEvent.parse("{\"event\":\"report\",\"run\":1}");
        assertNotNull(empty);
        assertEquals("", empty.line());
    }

    @Test
    @DisplayName("parses a run's states, with its directory, services, error and result")
    void parsesRuns() {
        HostEvent queued =
                HostEvent.parse(
                        "{\"event\":\"run\",\"dir\":\"/sandbox/parcels\",\"run\":4,"
                                + "\"command\":\"axx run --compact\",\"ide\":\"vscode\","
                                + "\"state\":\"queued\"}");
        assertNotNull(queued);
        assertEquals(HostEvent.RUN, queued.event());
        assertEquals(4, queued.run());
        assertEquals("axx run --compact", queued.command());
        assertEquals("vscode", queued.ide());
        assertEquals("/sandbox/parcels", queued.dir());
        assertEquals(HostEvent.QUEUED, queued.state());

        HostEvent started =
                HostEvent.parse(
                        "{\"event\":\"run\",\"services\":[\"api\"],\"run\":4,\"command\":\"axx run"
                                + " --compact\",\"state\":\"started\",\"seconds\":1.006033791}");
        assertNotNull(started);
        assertEquals(List.of("api"), started.services());
        assertEquals(1.006033791, started.seconds());

        HostEvent passed =
                HostEvent.parse(
                        "{\"event\":\"run\",\"run\":4,\"command\":\"axx run --compact\",\"state\":"
                                + "\"ended\",\"result\":{\"passed\":3,\"failed\":1,\"other\":0},"
                                + "\"seconds\":3.37}");
        assertNotNull(passed);
        assertEquals(new HostEvent.Result(3, 1, 0), passed.result());
        assertNull(passed.error());

        HostEvent refused =
                HostEvent.parse(
                        "{\"event\":\"run\",\"run\":5,\"command\":\"axx run --compact\","
                                + "\"state\":\"ended\",\"error\":{\"code\":\"AXX-E0501\","
                                + "\"message\":\"the person running the axx host did not approve"
                                + " the services' changed definition\",\"hint\":\"ask them\","
                                + "\"exit\":4},\"seconds\":0.05}");
        assertNotNull(refused);
        HostEvent.Failure error = refused.error();
        assertNotNull(error);
        assertEquals("AXX-E0501", error.code());
        assertEquals(4, error.exit());
        assertEquals("ask them", error.hint());
    }

    @Test
    @DisplayName("parses a report line, escapes and all")
    void parsesReport() {
        HostEvent report =
                HostEvent.parse(
                        "{\"event\":\"report\",\"run\":1,\"line\":\"##teamcity[testStdOut"
                                + " nodeId='129/127' out='log: GET /health -> 200|n{\\\"status\\\":"
                                + " \\\"ok\\\"}|n']\"}");
        assertNotNull(report);
        assertEquals(1, report.run());
        assertEquals(
                "##teamcity[testStdOut nodeId='129/127' out='log: GET /health -> 200|n{\"status\":"
                        + " \"ok\"}|n']",
                report.line());
    }

    @Test
    @DisplayName("parses an approval and its answer")
    void parsesApproval() {
        HostEvent approval = HostEvent.parse(APPROVAL);
        assertNotNull(approval);
        assertEquals(HostEvent.APPROVAL, approval.event());
        assertEquals(1, approval.id());
        assertEquals(1, approval.run());
        assertEquals("services:\n  api:\n    build: .\n", approval.approved());
        assertTrue(approval.proposed().endsWith("    volumes: [/tmp]\n"));
        assertTrue(approval.diff().startsWith("--- approved\n+++ proposed\n"));
        assertEquals(1, approval.warnings().size());

        HostEvent answered =
                HostEvent.parse(
                        "{\"event\":\"answered\",\"run\":1,\"id\":1,\"answer\":\"refused\"}");
        assertNotNull(answered);
        assertEquals(HostEvent.ANSWERED, answered.event());
        assertEquals("refused", answered.answer());
    }

    @Test
    @DisplayName("parses an approval the host asks as it starts, with no run")
    void parsesApprovalAtStart() {
        HostEvent approval = HostEvent.parse(START_APPROVAL);
        assertNotNull(approval);
        assertEquals(1, approval.id());
        assertEquals(0, approval.run());
        assertEquals("", approval.command());
        assertEquals(
                List.of("services.api: compose service api is privileged"), approval.warnings());

        HostEvent answered =
                HostEvent.parse("{\"event\":\"answered\",\"id\":1,\"answer\":\"abandoned\"}");
        assertNotNull(answered);
        assertEquals(0, answered.run());
        assertEquals("abandoned", answered.answer());
    }

    @Test
    @DisplayName("parses a line far longer than a console line")
    void parsesLongLines() {
        String big = "x".repeat(200_000);
        HostEvent approval =
                HostEvent.parse(
                        "{\"event\":\"approval\",\"id\":2,\"run\":3,\"proposed\":\"" + big + "\"}");
        assertNotNull(approval);
        assertEquals(big, approval.proposed());
    }

    @Test
    @DisplayName("ignores unknown fields, which later hosts may add")
    void ignoresUnknownFields() {
        HostEvent run =
                HostEvent.parse(
                        "{\"event\":\"run\",\"run\":2,\"state\":\"waiting\",\"queue\":[1],"
                                + "\"later\":{\"a\":1}}");
        assertNotNull(run);
        assertEquals(HostEvent.WAITING, run.state());
    }

    @ParameterizedTest
    @ValueSource(
            strings = {
                "",
                "   ",
                "axx: preparing rest (once; cached for later runs)",
                "[1, 2]",
                "\"serving\"",
                "{\"addr\":\"127.0.0.1:1\"}",
                "{\"event\":\"run\",\"run\":\"four\"}",
                "{\"event\":\"serving\""
            })
    @DisplayName("is null for what is not an event")
    void rejectsOthers(String line) {
        assertNull(HostEvent.parse(line));
    }
}
