// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * What the IDE knows of the axx host it started, from the host's events (see {@link HostEvent}),
 * read in order: whether it serves, the runs it serves until they end, and the approvals it asked
 * for that wait for an answer. It says what the status bar shows, and how the host's process ended.
 *
 * <p>It holds no IDE types, so it is unit-testable. Not thread-safe.
 */
public final class HostState {
    /** The code of the error an axx host exits with when another one serves the project. */
    static final String ALREADY_SERVED = "AXX-E0503";

    /** The usage error's code, which {@code axx host} exits with when there are no services. */
    static final String USAGE = "AXX-E0001";

    private static final int MAX_COMMAND_LENGTH = 40;
    private static final Pattern ERROR = Pattern.compile("\\[(AXX-E\\d{4})]:\\s*(.*)");

    /** Where the host is. */
    public enum Phase {
        /** Started, not serving yet (it may prepare the project's packs first). */
        STARTING,
        /** Serving the project's runs. */
        SERVING,
        /** Its process ended. */
        STOPPED,
        /** It did not start: another axx host, started in a terminal, serves the project. */
        ELSEWHERE
    }

    /** How the host's process ended. */
    public enum Exit {
        /** It stopped, or it failed after it served. */
        STOPPED,
        /** It did not start: another axx host serves the project. */
        ELSEWHERE,
        /** It did not start: axx.yaml declares no services. */
        NO_SERVICES,
        /** It did not start, for another reason (its standard error says). */
        FAILED
    }

    private Phase phase = Phase.STARTING;
    private @Nullable HostEvent serving;
    // By number: each run's last event, until it ended; the approvals, by id, until answered.
    private final Map<Integer, HostEvent> runs = new LinkedHashMap<>();
    private final Map<Integer, HostEvent> approvals = new LinkedHashMap<>();
    private @NotNull String problem = "";

    /** Reads an event. */
    public void apply(@NotNull HostEvent event) {
        switch (event.event()) {
            case HostEvent.SERVING -> {
                phase = Phase.SERVING;
                serving = event;
            }
            case HostEvent.RUN -> {
                // A question stays open when the run that asked ends: later runs wait for it.
                if (HostEvent.ENDED.equals(event.state())) {
                    runs.remove(event.run());
                } else {
                    runs.put(event.run(), event);
                }
            }
            case HostEvent.APPROVAL -> approvals.put(event.id(), event);
            case HostEvent.ANSWERED -> approvals.remove(event.id());
            default -> {}
        }
    }

    /**
     * The host's process ended. The runs it served and its questions are gone.
     *
     * @param exitCode its exit code
     * @param errors what it wrote on its standard error
     */
    public @NotNull Exit exited(int exitCode, @NotNull String errors) {
        Exit exit = exitOf(exitCode, errors, serving != null);
        phase = exit == Exit.ELSEWHERE ? Phase.ELSEWHERE : Phase.STOPPED;
        problem = exit == Exit.STOPPED && exitCode == 0 ? "" : errorMessage(errors);
        runs.clear();
        approvals.clear();
        return exit;
    }

    public @NotNull Phase phase() {
        return phase;
    }

    /** The {@code serving} event, once the host serves. */
    public @Nullable HostEvent serving() {
        return serving;
    }

    /** The question the host asked first that waits for an answer, or null when none does. */
    public @Nullable HostEvent pendingApproval() {
        return approvals.isEmpty() ? null : approvals.values().iterator().next();
    }

    /** A question that waits for an answer, or null when that one does not. */
    public @Nullable HostEvent approval(int id) {
        return approvals.get(id);
    }

    /**
     * The run the services are for: the one whose services start, run or stop, else the first one
     * that waits; null when there is none.
     */
    public @Nullable HostEvent currentRun() {
        HostEvent waiting = null;
        for (HostEvent run : runs.values()) {
            switch (run.state()) {
                case HostEvent.STARTING, HostEvent.STARTED, HostEvent.STOPPING,
                        HostEvent.APPROVAL_STATE -> {
                    return run;
                }
                default -> {
                    if (waiting == null) {
                        waiting = run;
                    }
                }
            }
        }
        return waiting;
    }

    /** The status bar's text: {@code axx host: } and the host's state. */
    public @NotNull String text() {
        return "axx host: " + switch (phase) {
            case STARTING -> approvals.isEmpty() ? "starting" : "waiting for your approval";
            case STOPPED -> "stopped";
            case ELSEWHERE -> "already running in a terminal";
            case SERVING -> servingText();
        };
    }

    private String servingText() {
        HostEvent run = currentRun();
        // A run may wait for a question an earlier run asked: its own state says so.
        if (!approvals.isEmpty()
                || (run != null && HostEvent.APPROVAL_STATE.equals(run.state()))) {
            return "waiting for your approval";
        }
        if (run == null) {
            return "ready";
        }
        String what =
                switch (run.state()) {
                    case HostEvent.STARTING -> "starting";
                    case HostEvent.STARTED -> "running";
                    case HostEvent.STOPPING -> "cleaning up";
                    default -> "queued";
                };
        return "run " + run.run() + ": " + shorten(run.command()) + " (" + what + ")";
    }

    /** The status bar's tooltip, as lines: where the host listens, its services and sources. */
    public @NotNull List<String> tooltip() {
        List<String> lines = new ArrayList<>();
        if (!problem.isEmpty()) {
            lines.add(problem);
        }
        if (phase == Phase.STARTING) {
            lines.add("Starting the axx host");
        }
        HostEvent run = phase == Phase.SERVING ? currentRun() : null;
        if (run != null) {
            lines.add("Run " + run.run() + ": " + run.command());
        }
        if (serving != null) {
            String where = phase == Phase.SERVING ? "Serving " : "Served ";
            lines.add(where + serving.dir() + " on " + serving.addr());
            lines.add("Services: " + String.join(", ", serving.services()));
            if (!serving.sources().isEmpty()) {
                lines.add("Sources: " + String.join(", ", serving.sources()));
            }
            if (serving.changed() && phase == Phase.SERVING) {
                lines.add("Changed since you last approved them");
            }
        }
        return lines;
    }

    /** A run's event as the host's terminal shows it, such as {@code run 4: ready in 2.1s}. */
    public static @NotNull String describe(@NotNull HostEvent run) {
        String what =
                switch (run.state()) {
                    case HostEvent.QUEUED -> run.command();
                    case HostEvent.WAITING -> "waits for the run before it";
                    case HostEvent.APPROVAL_STATE ->
                            "waits for your approval of the services' changed definition";
                    case HostEvent.STARTING -> "starting " + String.join(", ", run.services());
                    case HostEvent.STARTED -> "ready in " + seconds(run.seconds());
                    case HostEvent.STOPPING -> "stopping the services";
                    case HostEvent.ENDED -> ended(run);
                    default -> run.state();
                };
        return "run " + run.run() + ": " + what;
    }

    private static String ended(HostEvent run) {
        List<String> parts = new ArrayList<>();
        HostEvent.Result result = run.result();
        if (result != null) {
            int scenarios = result.passed() + result.failed() + result.other();
            parts.add(
                    scenarios
                            + (scenarios == 1 ? " scenario: " : " scenarios: ")
                            + result.passed()
                            + " passed, "
                            + result.failed()
                            + " failed");
        }
        HostEvent.Failure error = run.error();
        if (error != null) {
            String message = error.message() == null ? "failed" : error.message();
            String code = error.code();
            parts.add(code == null || code.isEmpty() ? message : message + " [" + code + "]");
        } else {
            parts.add("stopped and cleaned up in " + seconds(run.seconds()));
        }
        return String.join("; ", parts);
    }

    /** The exit code of a run that ended, as {@code axx run} would exit. */
    public static int exitCode(@NotNull HostEvent ended) {
        HostEvent.Failure error = ended.error();
        if (error != null) {
            return error.exit() != 0 ? error.exit() : 1;
        }
        HostEvent.Result result = ended.result();
        return result != null && result.failed() > 0 ? 1 : 0;
    }

    /**
     * How the host's process ended.
     *
     * @param served whether it served before it ended
     */
    static @NotNull Exit exitOf(int exitCode, @NotNull String errors, boolean served) {
        if (served) {
            return Exit.STOPPED;
        }
        String code = errorCode(errors);
        if (ALREADY_SERVED.equals(code)) {
            return Exit.ELSEWHERE;
        }
        if (USAGE.equals(code) && errors.contains("declares no services")) {
            return Exit.NO_SERVICES;
        }
        return exitCode == 0 ? Exit.STOPPED : Exit.FAILED;
    }

    /** The code of the first axx error in standard error text, such as {@code AXX-E0503}. */
    static @Nullable String errorCode(@NotNull String errors) {
        Matcher matcher = ERROR.matcher(errors);
        return matcher.find() ? matcher.group(1) : null;
    }

    /** The message of the first axx error in standard error text, else its last line. */
    static @NotNull String errorMessage(@NotNull String errors) {
        Matcher matcher = ERROR.matcher(errors);
        if (matcher.find()) {
            return matcher.group(2).strip();
        }
        String[] lines = errors.strip().split("\\R");
        return lines[lines.length - 1].strip();
    }

    private static String shorten(String command) {
        return command.length() <= MAX_COMMAND_LENGTH
                ? command
                : command.substring(0, MAX_COMMAND_LENGTH - 1) + "…";
    }

    private static String seconds(double seconds) {
        return String.format(Locale.ROOT, "%.1fs", seconds);
    }
}
