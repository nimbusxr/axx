// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import us.nimbusxr.axx.idea.web.FakePausedScenario.Request;
import us.nimbusxr.axx.idea.web.PausedScenarioClient.Answer;

import java.io.IOException;
import java.net.http.HttpTimeoutException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;

@DisplayName("PausedScenarioClient")
class PausedScenarioClientTest {
    private final PausedScenarioClient client = new PausedScenarioClient();
    private FakePausedScenario scenario;

    @BeforeEach
    void start() throws IOException {
        scenario = new FakePausedScenario();
    }

    @AfterEach
    void stop() {
        scenario.close();
    }

    @Test
    @DisplayName("posts a step's text to highlight, and gives the answer")
    void highlight() throws Exception {
        scenario.answer("POST", "highlight", 200, "the button named \"Get a quote\"");
        assertEquals(
                new Answer(200, "the button named \"Get a quote\""),
                client.highlight(scenario.url, "And the \"Get a quote\" button is clicked")
                        .get(5, TimeUnit.SECONDS));
        assertEquals(
                new Request("POST", "highlight", "And the \"Get a quote\" button is clicked"),
                scenario.next());

        scenario.answer("POST", "highlight", 404, "No button named \"Send\" on the page; 1 buttons: ...");
        assertEquals(
                404,
                client.highlight(scenario.url, "And the \"Send\" button is clicked")
                        .get(5, TimeUnit.SECONDS)
                        .status());
    }

    @Test
    @DisplayName("gives up on a highlight after 2 seconds")
    void highlightTimesOut() {
        scenario.answer("POST", "highlight", 200, "late", 4000);
        ExecutionException e =
                assertThrows(
                        ExecutionException.class,
                        () -> client.highlight(scenario.url, "When x").get(10, TimeUnit.SECONDS));
        assertInstanceOf(HttpTimeoutException.class, e.getCause());
    }

    @Test
    @DisplayName("runs a step with its table, as long as it takes")
    void run() throws Exception {
        scenario.answer("POST", "run", 200, "passed", 2500);
        String step = "When the form is filled in with:\n| Weight | 2 kg |";
        assertEquals(new Answer(200, "passed"), client.run(scenario.url, step).get(10, TimeUnit.SECONDS));
        assertEquals(new Request("POST", "run", step), scenario.next());

        scenario.answer("POST", "run", 422, "No button named \"Send\" on the page");
        assertEquals(
                new Answer(422, "No button named \"Send\" on the page"),
                client.run(scenario.url, "And the \"Send\" button is clicked").get(5, TimeUnit.SECONDS));
    }

    @Test
    @DisplayName("gets the recorded steps from the run, else from the recording file")
    void recordedSteps(@TempDir Path dir) throws Exception {
        Path recording = dir.resolve("recording.txt");
        Files.writeString(
                recording, "# Steps recorded in Playwright's Inspector\n    When the page \"/quote\" is opened\n");
        scenario.answer("GET", "recorded", 200, "    And the \"Get a quote\" button is clicked\n");

        assertEquals(
                "    And the \"Get a quote\" button is clicked\n",
                client.recordedSteps(scenario.url, recording));
        assertEquals(new Request("GET", "recorded", ""), scenario.next());

        // Not answered, or the run has ended: the file.
        scenario.answer("GET", "recorded", 500, "oops");
        assertEquals("    When the page \"/quote\" is opened\n", client.recordedSteps(scenario.url, recording));
        String ended = scenario.url;
        scenario.close();
        assertEquals("    When the page \"/quote\" is opened\n", client.recordedSteps(ended, recording));
        assertEquals("    When the page \"/quote\" is opened\n", client.recordedSteps(null, recording));

        assertNull(client.recordedSteps(null, dir.resolve("missing.txt")));
        assertNull(client.recordedSteps(null, null));
    }

    @Test
    @DisplayName("sends nothing to a URL off the loopback interface")
    void onlyLoopback() {
        ExecutionException e =
                assertThrows(
                        ExecutionException.class,
                        () -> client.run("http://example.com/3f9c0a/", "When x").get(5, TimeUnit.SECONDS));
        assertInstanceOf(IOException.class, e.getCause());
    }
}
