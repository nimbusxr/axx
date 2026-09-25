// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import org.jetbrains.annotations.NotNull;

import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Knows which {@code "Debugger: <app>"} run configurations are live in a project: scheduled,
 * running, or just requested by this plugin. The plugin checks it before starting a debugger so it
 * never starts a second instance of one, for example when the {@code "axx: debug all"} compound
 * starts the debugger configurations and the axx run at the same time.
 *
 * <p>It is fed from execution events and holds no IDE types, so its rules are unit-testable.
 */
final class DebuggerRegistry {
    /** How long a start this plugin requested counts as live before the IDE schedules it. */
    static final long REQUEST_GRACE_MILLIS = 10_000;

    // Configuration name -> launches (one token per execution) scheduled and not yet ended.
    private final Map<String, Set<Object>> launches = new ConcurrentHashMap<>();
    // Configuration name -> when this plugin asked the IDE to start it, until the IDE schedules it.
    private final Map<String, Long> requests = new ConcurrentHashMap<>();

    /** Whether a run configuration is a debugger configuration this plugin starts. */
    static boolean isDebuggerConfiguration(@NotNull String runProfileName) {
        return runProfileName.startsWith(DebugRequest.CONFIGURATION_PREFIX);
    }

    /**
     * Whether to watch a process's console for debugger requests. axx is usually started from a
     * Shell Script configuration with the Run executor, so processes started with any executor are
     * watched, except the debugger configurations themselves.
     *
     * @param executorId the executor the process was started with (Run, Debug, ...), not used
     * @param runProfileName the name of the process's run configuration
     */
    static boolean shouldWatch(@NotNull String executorId, @NotNull String runProfileName) {
        return !isDebuggerConfiguration(runProfileName);
    }

    /** The IDE scheduled a launch of a run configuration. */
    void launchScheduled(@NotNull String runProfileName, @NotNull Object launch) {
        if (!isDebuggerConfiguration(runProfileName)) {
            return;
        }
        launches.computeIfAbsent(runProfileName, k -> ConcurrentHashMap.newKeySet()).add(launch);
        requests.remove(runProfileName);
    }

    /** A launch ended: it terminated, or it never started. Unknown launches are ignored. */
    void launchEnded(@NotNull String runProfileName, @NotNull Object launch) {
        launches.computeIfPresent(
                runProfileName,
                (name, live) -> {
                    live.remove(launch);
                    return live.isEmpty() ? null : live;
                });
    }

    /** This plugin asked the IDE to start a debugger configuration. */
    void startRequested(@NotNull String configurationName, long nowMillis) {
        requests.put(configurationName, nowMillis);
    }

    /**
     * Whether a debugger configuration is scheduled or running, or this plugin requested it within
     * {@link #REQUEST_GRACE_MILLIS}.
     */
    boolean isLive(@NotNull String configurationName, long nowMillis) {
        if (launches.containsKey(configurationName)) {
            return true;
        }
        Long requested = requests.get(configurationName);
        return requested != null && nowMillis - requested < REQUEST_GRACE_MILLIS;
    }
}
