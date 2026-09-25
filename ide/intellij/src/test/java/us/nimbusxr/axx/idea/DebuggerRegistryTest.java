// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

@DisplayName("DebuggerRegistry")
class DebuggerRegistryTest {
    private static final String API = "Debugger: api";

    private final DebuggerRegistry registry = new DebuggerRegistry();

    @ParameterizedTest(name = "{0}")
    @DisplayName("watches processes started with any executor")
    @ValueSource(strings = {"Run", "Debug", "Coverage", "Profiler"})
    void watchesAnyExecutor(String executorId) {
        assertTrue(DebuggerRegistry.shouldWatch(executorId, "axx: debug"));
        assertTrue(DebuggerRegistry.shouldWatch(executorId, "acceptance tests"));
    }

    @ParameterizedTest(name = "{0}")
    @DisplayName("never watches debugger configurations")
    @ValueSource(strings = {"Run", "Debug"})
    void skipsDebuggerConfigurations(String executorId) {
        assertFalse(DebuggerRegistry.shouldWatch(executorId, API));
        assertFalse(DebuggerRegistry.shouldWatch(executorId, "Debugger: parcels"));
    }

    @Test
    @DisplayName("a debugger started by the compound is live, so it is not started again")
    void compoundStartedDebuggerIsLive() {
        // "axx: debug all" schedules the debuggers and "axx: debug" together; axx's request
        // arrives while the debugger is starting or running.
        registry.launchScheduled(API, new Object());
        registry.launchScheduled("axx: debug", new Object());

        assertTrue(registry.isLive(API, 0));
    }

    @Test
    @DisplayName("a debugger is no longer live once its launch ends")
    void endedLaunchIsNotLive() {
        Object launch = new Object();
        registry.launchScheduled(API, launch);
        registry.launchEnded(API, launch);

        assertFalse(registry.isLive(API, 0));
    }

    @Test
    @DisplayName("an unrelated launch that never started does not clear a live one")
    void unknownLaunchEndIsIgnored() {
        registry.launchScheduled(API, new Object());
        registry.launchEnded(API, new Object());

        assertTrue(registry.isLive(API, 0));
    }

    @Test
    @DisplayName("stays live until every parallel launch ends")
    void parallelLaunches() {
        Object first = new Object();
        Object second = new Object();
        registry.launchScheduled(API, first);
        registry.launchScheduled(API, second);

        registry.launchEnded(API, first);
        assertTrue(registry.isLive(API, 0));

        registry.launchEnded(API, second);
        assertFalse(registry.isLive(API, 0));
    }

    @Test
    @DisplayName("only tracks debugger configurations")
    void ignoresOtherConfigurations() {
        registry.launchScheduled("axx: debug", new Object());

        assertFalse(registry.isLive("axx: debug", 0));
    }

    @Test
    @DisplayName("a requested start is live until the grace period ends")
    void requestIsLiveDuringGrace() {
        registry.startRequested(API, 1_000);

        assertTrue(registry.isLive(API, 1_000));
        assertTrue(registry.isLive(API, 1_000 + DebuggerRegistry.REQUEST_GRACE_MILLIS - 1));
        assertFalse(registry.isLive(API, 1_000 + DebuggerRegistry.REQUEST_GRACE_MILLIS));
    }

    @Test
    @DisplayName(
            "once scheduled, the launch supersedes the request, so a quick failure can be retried")
    void scheduledLaunchSupersedesRequest() {
        Object launch = new Object();
        registry.startRequested(API, 0);
        registry.launchScheduled(API, launch);
        registry.launchEnded(API, launch);

        assertFalse(registry.isLive(API, 1));
    }
}
