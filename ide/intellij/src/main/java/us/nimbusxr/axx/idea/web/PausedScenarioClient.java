// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * Talks to a paused scenario at the URL its run announced (see {@link ScenarioPause}), over HTTP on
 * the loopback interface, with plain text bodies:
 *
 * <ul>
 *   <li>{@code POST <url>highlight} with a step's text highlights the element it names on the
 *       paused page: 200 with a description of it, 404 with what the step would fail with, 409
 *       when no scenario is paused;
 *   <li>{@code POST <url>run} with a step's text, and the rows of its table on the lines below,
 *       runs it in the paused scenario: 200 {@code passed}, 422 with its failure, 409 when no
 *       scenario is paused;
 *   <li>{@code GET <url>recorded}: the steps Playwright's Inspector recorded so far.
 * </ul>
 *
 * <p>It holds no IDE state, so it is unit-testable.
 */
public final class PausedScenarioClient {
    /** How long a highlight or the recorded steps may take. */
    static final Duration QUICK = Duration.ofSeconds(2);

    /** An answer: its HTTP status and its text. */
    public record Answer(int status, @NotNull String text) {}

    private final HttpClient http =
            HttpClient.newBuilder()
                    .proxy(HttpClient.Builder.NO_PROXY)
                    .version(HttpClient.Version.HTTP_1_1)
                    .connectTimeout(QUICK)
                    .build();

    /** Highlights the element a step names on the paused page. */
    public @NotNull CompletableFuture<Answer> highlight(@NotNull String url, @NotNull String step) {
        return send(url, "highlight", step, QUICK);
    }

    /**
     * Runs a step, with the rows of its table on the lines below it, in the paused scenario. It
     * takes as long as the step does.
     */
    public @NotNull CompletableFuture<Answer> run(@NotNull String url, @NotNull String step) {
        return send(url, "run", step, null);
    }

    /** The steps recorded in Playwright's Inspector so far. */
    public @NotNull CompletableFuture<Answer> recorded(@NotNull String url) {
        return send(url, "recorded", null, QUICK);
    }

    /**
     * The recorded steps to insert: from the run while it runs ({@code liveUrl}), else from the
     * recording file of its last pause, without the file's first comment line; null when neither
     * has them.
     */
    public @Nullable String recordedSteps(@Nullable String liveUrl, @Nullable Path recording)
            throws IOException {
        if (liveUrl != null) {
            try {
                Answer answer = recorded(liveUrl).get(QUICK.toMillis() + 1000, TimeUnit.MILLISECONDS);
                if (answer.status() == 200) {
                    return answer.text();
                }
            } catch (ExecutionException | TimeoutException e) {
                // The run has ended: its file has the steps.
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return null;
            }
        }
        if (recording == null || !Files.isRegularFile(recording)) {
            return null;
        }
        return FeatureSteps.fromRecordingFile(Files.readString(recording, StandardCharsets.UTF_8));
    }

    private CompletableFuture<Answer> send(
            String url, String path, @Nullable String body, @Nullable Duration timeout) {
        if (!ScenarioPause.isLoopbackUrl(url)) {
            return CompletableFuture.failedFuture(
                    new IOException("not a paused scenario's URL: " + url));
        }
        HttpRequest.Builder request = HttpRequest.newBuilder(URI.create(url + path));
        if (timeout != null) {
            request.timeout(timeout);
        }
        if (body != null) {
            request.header("Content-Type", "text/plain; charset=utf-8")
                    .POST(HttpRequest.BodyPublishers.ofString(body, StandardCharsets.UTF_8));
        } else {
            request.GET();
        }
        return http.sendAsync(request.build(), HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8))
                .thenApply(response -> new Answer(response.statusCode(), response.body()));
    }
}
