// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.util.List;

/** What the status bar and the run's console say, from the host's events. */
@DisplayName("HostState")
class HostStateTest {
    private static final String SERVING =
            "{\"event\":\"serving\",\"addr\":\"127.0.0.1:51621\",\"dir\":\"/work/parcels\","
                    + "\"services\":[\"api\",\"db\"],\"sources\":[\"compose.yaml\"]}";
    private static final String ALREADY_SERVED =
            "error[AXX-E0503]: an axx host already serves /work/parcels (pid 18719,"
                    + " 127.0.0.1:51727)\n"
                    + "  hint: its runs are yours too: stop it first to start another\n"
                    + "  docs: https://axx.nimbusxr.us/references/error-codes/#axx-e0503\n";

    private final HostState state = new HostState();

    private void apply(String line) {
        HostEvent event = HostEvent.parse(line);
        assertNotNull(event, line);
        state.apply(event);
    }

    private static String run(int run, String state) {
        return "{\"event\":\"run\",\"run\":"
                + run
                + ",\"command\":\"axx run --compact"
                + " features/parcels.feature\",\"state\":\""
                + state
                + "\"}";
    }

    @Test
    @DisplayName("says the host starts, then serves, and what on hover")
    void serving() {
        assertEquals("axx host: starting", state.text());
        assertEquals(List.of("Starting the axx host"), state.tooltip());

        apply(SERVING);
        assertEquals(HostState.Phase.SERVING, state.phase());
        assertEquals("axx host: ready", state.text());
        assertEquals(
                List.of(
                        "Serving /work/parcels on 127.0.0.1:51621",
                        "Services: api, db",
                        "Sources: compose.yaml"),
                state.tooltip());
    }

    @Test
    @DisplayName("says when the definition changed since it was approved")
    void changed() {
        apply(
                "{\"event\":\"serving\",\"addr\":\"127.0.0.1:1\",\"dir\":\"/work/parcels\","
                        + "\"services\":[\"api\"],\"changed\":true}");
        assertEquals(
                List.of(
                        "Serving /work/parcels on 127.0.0.1:1",
                        "Services: api",
                        "Changed since you last approved them"),
                state.tooltip());
    }

    @Test
    @DisplayName("follows a run from queued to ended")
    void runs() {
        apply(SERVING);
        apply(run(4, "queued"));
        assertEquals(
                "axx host: run 4: axx run --compact features/parcels.feat… (queued)",
                state.text());
        apply(run(4, "starting"));
        assertEquals(
                "axx host: run 4: axx run --compact features/parcels.feat… (starting)",
                state.text());
        assertEquals("Run 4: axx run --compact features/parcels.feature", state.tooltip().get(0));
        apply(run(4, "started"));
        assertEquals(
                "axx host: run 4: axx run --compact features/parcels.feat… (running)",
                state.text());
        // The next run waits for its turn: the status bar stays with the one that has it.
        apply(run(5, "queued"));
        apply(run(5, "waiting"));
        assertEquals(4, state.currentRun().run());
        apply(run(4, "stopping"));
        assertEquals(
                "axx host: run 4: axx run --compact features/parcels.feat… (cleaning up)",
                state.text());
        apply(run(4, "ended"));
        assertEquals(5, state.currentRun().run());
        apply(run(5, "ended"));
        assertNull(state.currentRun());
        assertEquals("axx host: ready", state.text());
    }

    @Test
    @DisplayName("waits for the person's approval until it is answered")
    void approvals() {
        apply(SERVING);
        apply(run(1, "queued"));
        apply(run(1, "approval"));
        assertEquals("axx host: waiting for your approval", state.text());
        apply(HostEventTest.APPROVAL);
        HostEvent question = state.pendingApproval();
        assertNotNull(question);
        assertEquals(1, question.id());
        assertNotNull(state.approval(1));
        assertNull(state.approval(2));

        apply("{\"event\":\"answered\",\"run\":1,\"id\":1,\"answer\":\"approved\"}");
        assertNull(state.pendingApproval());
        apply(run(1, "starting"));
        assertEquals(
                "axx host: run 1: axx run --compact features/parcels.feat… (starting)",
                state.text());
    }

    @Test
    @DisplayName("keeps a question open after its run stops waiting, until it is answered")
    void openQuestions() {
        apply(SERVING);
        apply(run(1, "approval"));
        apply(HostEventTest.APPROVAL);
        apply(
                "{\"event\":\"run\",\"run\":1,\"state\":\"ended\",\"error\":{\"code\":"
                        + "\"AXX-E0507\",\"message\":\"not answered yet\",\"exit\":4}}");
        assertNotNull(state.pendingApproval());
        assertEquals("axx host: waiting for your approval", state.text());
        // A later run of the same definition waits for it, with no question of its own.
        apply(run(2, "queued"));
        apply(run(2, "approval"));
        assertEquals(1, state.pendingApproval().id());
        // A newer question replaces it.
        apply("{\"event\":\"answered\",\"run\":1,\"id\":1,\"answer\":\"abandoned\"}");
        assertNull(state.pendingApproval());
        assertEquals("axx host: waiting for your approval", state.text());
        apply(run(2, "ended"));
        assertEquals("axx host: ready", state.text());
    }

    @Test
    @DisplayName("waits for the person's approval of a change the host found as it started")
    void questionAtStart() {
        apply(HostEventTest.START_APPROVAL);
        assertEquals("axx host: waiting for your approval", state.text());
        apply(SERVING);
        assertEquals("axx host: waiting for your approval", state.text());
        HostEvent question = state.pendingApproval();
        assertNotNull(question);
        assertEquals(0, question.run());
        apply("{\"event\":\"answered\",\"id\":1,\"answer\":\"approved\"}");
        assertEquals("axx host: ready", state.text());
    }

    @Test
    @DisplayName("says the host stopped, and the runs and questions are gone")
    void stopped() {
        apply(SERVING);
        apply(run(1, "approval"));
        apply(HostEventTest.APPROVAL);
        assertEquals(HostState.Exit.STOPPED, state.exited(0, ""));
        assertEquals("axx host: stopped", state.text());
        assertNull(state.pendingApproval());
        assertNull(state.currentRun());
        assertEquals("Served /work/parcels on 127.0.0.1:51621", state.tooltip().get(0));
    }

    @Test
    @DisplayName("says another host serves the project, started in a terminal")
    void elsewhere() {
        assertEquals(HostState.Exit.ELSEWHERE, state.exited(4, ALREADY_SERVED));
        assertEquals(HostState.Phase.ELSEWHERE, state.phase());
        assertEquals("axx host: already running in a terminal", state.text());
        assertEquals(
                List.of("an axx host already serves /work/parcels (pid 18719, 127.0.0.1:51727)"),
                state.tooltip());
    }

    @Test
    @DisplayName("tells why a host did not start")
    void exits() {
        assertEquals(
                HostState.Exit.NO_SERVICES,
                HostState.exitOf(
                        2,
                        "Error[AXX-E0001]: axx.yaml declares no services: an axx host has nothing"
                                + " to start\n  hint: declare the services runs start under"
                                + " services: in axx.yaml\n",
                        false));
        assertEquals(
                HostState.Exit.FAILED,
                HostState.exitOf(2, "Error[AXX-E0001]: unknown flag: --nope\n", false));
        assertEquals(HostState.Exit.FAILED, HostState.exitOf(1, "panic: boom", false));
        // Once it served, it stopped, whatever it says.
        assertEquals(HostState.Exit.STOPPED, HostState.exitOf(4, ALREADY_SERVED, true));

        assertEquals("AXX-E0503", HostState.errorCode(ALREADY_SERVED));
        assertNull(HostState.errorCode("panic: boom"));
        assertEquals("boom", HostState.errorMessage("goroutine 1\n  boom \n"));

        HostState failed = new HostState();
        assertEquals(
                HostState.Exit.FAILED,
                failed.exited(
                        4,
                        "error[AXX-E0504]: services.api.command: `docker compose config`"
                            + " failed\n"));
        assertEquals("axx host: stopped", failed.text());
        assertEquals(
                List.of("services.api.command: `docker compose config` failed"), failed.tooltip());
    }

    @Test
    @DisplayName("describes a run's events as the host's terminal does")
    void describes() {
        assertEquals(
                "run 4: axx run --compact features/parcels.feature",
                HostState.describe(HostEvent.parse(run(4, "queued"))));
        assertEquals(
                "run 4: waits for the run before it",
                HostState.describe(HostEvent.parse(run(4, "waiting"))));
        assertEquals(
                "run 4: waits for your approval of the services' changed definition",
                HostState.describe(HostEvent.parse(run(4, "approval"))));
        assertEquals(
                "run 4: starting api, db",
                HostState.describe(
                        HostEvent.parse(
                                "{\"event\":\"run\",\"run\":4,\"state\":\"starting\","
                                        + "\"services\":[\"api\",\"db\"]}")));
        assertEquals(
                "run 4: ready in 1.0s",
                HostState.describe(
                        HostEvent.parse(
                                "{\"event\":\"run\",\"run\":4,\"state\":\"started\","
                                        + "\"seconds\":1.006}")));
        assertEquals(
                "run 4: stopping the services",
                HostState.describe(HostEvent.parse(run(4, "stopping"))));

        HostEvent passed =
                HostEvent.parse(
                        "{\"event\":\"run\",\"run\":4,\"state\":\"ended\",\"result\":{\"passed\":1,"
                                + "\"failed\":0,\"other\":0},\"seconds\":3.374}");
        assertEquals(
                "run 4: 1 scenario: 1 passed, 0 failed; stopped and cleaned up in 3.4s",
                HostState.describe(passed));
        assertEquals(0, HostState.exitCode(passed));

        HostEvent failed =
                HostEvent.parse(
                        "{\"event\":\"run\",\"run\":4,\"state\":\"ended\",\"result\":{\"passed\":2,"
                                + "\"failed\":1,\"other\":1},\"seconds\":2}");
        assertEquals(
                "run 4: 4 scenarios: 2 passed, 1 failed; stopped and cleaned up in 2.0s",
                HostState.describe(failed));
        assertEquals(1, HostState.exitCode(failed));

        HostEvent refused =
                HostEvent.parse(
                        "{\"event\":\"run\",\"run\":5,\"state\":\"ended\",\"error\":{\"code\":"
                                + "\"AXX-E0501\",\"message\":\"the person running the axx host did"
                                + " not approve the services' changed definition\",\"exit\":4},"
                                + "\"seconds\":0.05}");
        assertEquals(
                "run 5: the person running the axx host did not approve the services' changed"
                        + " definition [AXX-E0501]",
                HostState.describe(refused));
        assertEquals(4, HostState.exitCode(refused));

        HostEvent unanswered =
                HostEvent.parse(
                        "{\"event\":\"run\",\"run\":6,\"state\":\"ended\",\"error\":{\"code\":"
                                + "\"AXX-E0507\",\"message\":\"the person running the axx host"
                                + " has not answered yet; the question stays open where the host"
                                + " runs\",\"exit\":4},\"seconds\":60.1}");
        assertEquals(
                "run 6: the person running the axx host has not answered yet; the question stays"
                        + " open where the host runs [AXX-E0507]",
                HostState.describe(unanswered));
        assertEquals(4, HostState.exitCode(unanswered));
    }
}
