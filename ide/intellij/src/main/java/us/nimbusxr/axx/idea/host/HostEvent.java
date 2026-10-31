// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import com.google.gson.Gson;
import com.google.gson.JsonElement;
import com.google.gson.JsonParseException;
import com.google.gson.JsonParser;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.List;

/**
 * A line {@code axx host --ide} writes on its standard output: one JSON object, what happened in
 * the host. Which fields an event has depends on its kind ({@link #event}):
 *
 * <ul>
 *   <li>{@code serving}: {@code addr}, {@code dir} (the project), {@code services}, {@code sources}
 *       (the files the services' commands name, relative), {@code warnings} (what in them reaches
 *       past a container) and {@code changed} (the definition changed since the person last
 *       approved one: the host asks them);
 *   <li>{@code run}: {@code run}, {@code command}, {@code ide} (the {@code AXX_IDE} of the process
 *       that started the run; empty for an agent's or a terminal's), {@code dir} (on {@code
 *       queued}: the run's project directory as the run sees it), {@code state} (see {@link
 *       #QUEUED} and the other states), {@code services} and {@code seconds}, and on {@code ended}
 *       its {@code error} and {@code result};
 *   <li>{@code report}: {@code run} and {@code line}, a line of the run's TeamCity report;
 *   <li>{@code approval}: {@code id}, {@code run} and {@code command} (of the run that asked; none
 *       when the host asked as it started), {@code approved} (the definition the person last
 *       approved; may be empty), {@code proposed}, {@code diff} and {@code warnings}: a question
 *       for the person, which the editor answers on the host's standard input. It stays open until
 *       answered, after the run that asked stopped waiting too, and later runs of the same
 *       definition wait for it (their {@code approval} state comes with no question of their own);
 *   <li>{@code answered}: {@code id}, {@code run} and {@code answer} ({@code approved}, {@code
 *       refused} or {@code abandoned}, when a newer question replaced it or the host stops).
 * </ul>
 *
 * <p>Absent strings are empty, absent lists empty, absent numbers 0. It holds no IDE types, so it
 * is unit-testable.
 */
public record HostEvent(
        @NotNull String event,
        @NotNull String addr,
        @NotNull String dir,
        @NotNull List<String> services,
        @NotNull List<String> sources,
        @NotNull List<String> warnings,
        boolean changed,
        int run,
        @NotNull String command,
        @NotNull String ide,
        @NotNull String state,
        @Nullable Failure error,
        @Nullable Result result,
        double seconds,
        @NotNull String line,
        int id,
        @NotNull String approved,
        @NotNull String proposed,
        @NotNull String diff,
        @NotNull String answer) {

    // The kinds of events.
    public static final String SERVING = "serving";
    public static final String RUN = "run";
    public static final String REPORT = "report";
    public static final String APPROVAL = "approval";
    public static final String ANSWERED = "answered";

    // A run's states, in the order they come. Queued: it connected. Waiting: for the run before it
    // to be done with the services. Approval: for the person to approve the services' changed
    // definition. Starting: the host starts its services. Started: they are ready, its scenarios
    // run. Stopping: the host stops them and cleans up after them. Ended: it is done.
    public static final String QUEUED = "queued";
    public static final String WAITING = "waiting";
    public static final String APPROVAL_STATE = "approval";
    public static final String STARTING = "starting";
    public static final String STARTED = "started";
    public static final String STOPPING = "stopping";
    public static final String ENDED = "ended";

    private static final Gson GSON = new Gson();

    /** Why a run failed, as axx reports errors. */
    public record Failure(
            @Nullable String code, @Nullable String message, @Nullable String hint, int exit) {}

    /** A run's scenarios, by how they ended. */
    public record Result(int passed, int failed, int other) {}

    public HostEvent {
        event = text(event);
        addr = text(addr);
        dir = text(dir);
        services = list(services);
        sources = list(sources);
        warnings = list(warnings);
        command = text(command);
        ide = text(ide);
        state = text(state);
        line = text(line);
        approved = text(approved);
        proposed = text(proposed);
        diff = text(diff);
        answer = text(answer);
    }

    /**
     * Parses a line of the host's standard output.
     *
     * @return the event, or null when the line is not a JSON object with an {@code event}
     */
    public static @Nullable HostEvent parse(@Nullable String line) {
        if (line == null || line.isBlank()) {
            return null;
        }
        try {
            JsonElement json = JsonParser.parseString(line);
            if (!json.isJsonObject()) {
                return null;
            }
            HostEvent event = GSON.fromJson(json, HostEvent.class);
            return event == null || event.event().isEmpty() ? null : event;
        } catch (JsonParseException | IllegalStateException | NumberFormatException e) {
            return null;
        }
    }

    private static String text(@Nullable String value) {
        return value == null ? "" : value;
    }

    private static List<String> list(@Nullable List<String> values) {
        if (values == null) {
            return List.of();
        }
        return values.stream().map(HostEvent::text).toList();
    }
}
