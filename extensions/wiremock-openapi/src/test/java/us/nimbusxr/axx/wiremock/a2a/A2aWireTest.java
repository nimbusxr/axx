// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.github.tomakehurst.wiremock.WireMockServer;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.net.http.HttpResponse;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;

/** The mock's answers as they go over the wire, in both bindings: their envelopes, streams and errors. */
class A2aWireTest extends A2aMockTest {
    @Test
    @SuppressWarnings("unchecked")
    void jsonRpcAnswersEchoTheIdAndStreamsAreEventsOfResponses() throws Exception {
        answerAbout("PX-A2A-9101", "{\"state\": \"completed\", \"updates\": [\"Looking up PX-A2A-9101\"]}");
        HttpResponse<String> res = send("POST", JSONRPC, rpc("req-1", "SendMessage", message("Where is PX-A2A-9101?")));
        assertThat(res.statusCode()).isEqualTo(200);
        Map<String, Object> answer = json(res.body());
        assertThat(answer).containsEntry("jsonrpc", "2.0").containsEntry("id", "req-1").containsKey("result");
        Map<String, Object> task = (Map<String, Object>) ((Map<String, Object>) answer.get("result")).get("task");
        assertThat((Map<String, Object>) task.get("status")).containsEntry("state", "TASK_STATE_COMPLETED").containsKey("timestamp");

        res = send("POST", JSONRPC, rpc(7, "SendStreamingMessage", message("Where is PX-A2A-9101?")));
        assertThat(res.headers().firstValue("Content-Type")).hasValue("text/event-stream");
        List<Map<String, Object>> events = events(res.body());
        assertThat(events).allSatisfy(e -> assertThat(e).containsEntry("jsonrpc", "2.0").containsEntry("id", 7));
        assertThat(events).extracting(e -> ((Map<String, Object>) e.get("result")).keySet().iterator().next())
                .containsExactly("task", "statusUpdate", "statusUpdate");
    }

    @Test
    @SuppressWarnings("unchecked")
    void restAnswersAreTheProtosJson() throws Exception {
        answerAbout("PX-A2A-9102", "{\"reply\": \"PX-A2A-9102 is out for delivery.\"}");
        HttpResponse<String> res = send("POST", REST + "/message:send", message("Where is PX-A2A-9102?"));
        assertThat(res.statusCode()).isEqualTo(200);
        assertThat(res.headers().firstValue("Content-Type")).hasValue("application/a2a+json");
        Map<String, Object> reply = (Map<String, Object>) json(res.body()).get("message");
        assertThat(reply).containsEntry("role", "ROLE_AGENT").containsEntry("parts", List.of(Map.of("text", "PX-A2A-9102 is out for delivery.")));

        res = send("POST", REST + "/message:stream", message("Where is PX-A2A-9102?"));
        assertThat(events(res.body())).singleElement().satisfies(e -> assertThat(e).containsOnlyKeys("message"));
    }

    @Test
    @SuppressWarnings("unchecked")
    void restErrorsAreStatusesWithTheirErrorInfo() throws Exception {
        HttpResponse<String> res = send("GET", REST + "/tasks/no-such-task", null);
        assertThat(res.statusCode()).isEqualTo(404);
        Map<String, Object> error = (Map<String, Object>) json(res.body()).get("error");
        assertThat(error).containsEntry("code", 404).containsEntry("status", "NOT_FOUND").containsEntry("message", "Task not found: no-such-task");
        assertThat((List<Map<String, Object>>) error.get("details")).containsExactly(Map.of("@type", "type.googleapis.com/google.rpc.ErrorInfo",
                "reason", "TASK_NOT_FOUND", "domain", "a2a-protocol.org"));

        res = send("POST", REST + "/message:send", message("Where is PX-A2A-9103?"));
        assertThat(res.statusCode()).isEqualTo(500);
        assertThat((Map<String, Object>) json(res.body()).get("error")).containsEntry("message", "no stub answers the message: Where is PX-A2A-9103?");

        res = send("POST", REST + "/tasks/any/pushNotificationConfigs", "{\"url\": \"https://shop.example/hooks/a2a\"}");
        assertThat(res.statusCode()).isEqualTo(400);
        assertThat(res.body()).contains("PUSH_NOTIFICATION_NOT_SUPPORTED");

        res = send("GET", REST + "/extendedAgentCard", null);
        assertThat(res.statusCode()).isEqualTo(400);
        assertThat(res.body()).contains("UNSUPPORTED_OPERATION");

        res = send("DELETE", REST + "/message:send", null);
        assertThat(res.statusCode()).isEqualTo(405);
    }

    @Test
    @SuppressWarnings("unchecked")
    void jsonRpcErrors() throws Exception {
        Map<String, Object> error = (Map<String, Object>) json(send("POST", JSONRPC, rpc(1, "GetTask", "{\"id\": \"no-such-task\"}")).body()).get("error");
        assertThat(error).containsEntry("code", -32001);
        error = (Map<String, Object>) json(send("POST", JSONRPC, rpc(2, "CreateTaskPushNotificationConfig", "{\"taskId\": \"t\", \"url\": \"https://shop.example/hooks\"}")).body()).get("error");
        assertThat(error).containsEntry("code", -32003);
        error = (Map<String, Object>) json(send("POST", JSONRPC, rpc(3, "GetExtendedAgentCard", "{}")).body()).get("error");
        assertThat(error).containsEntry("code", -32004);
        error = (Map<String, Object>) json(send("POST", JSONRPC, rpc(4, "message/send", "{}")).body()).get("error");
        assertThat(error).containsEntry("code", -32601);
        error = (Map<String, Object>) json(send("POST", JSONRPC, "{not json").body()).get("error");
        assertThat(error).containsEntry("code", -32700);
        error = (Map<String, Object>) json(send("POST", JSONRPC, rpc(5, "SendMessage", "{\"message\": {\"role\": \"ROLE_USER\", \"parts\": [{\"text\": \"hi\"}]}}")).body()).get("error");
        assertThat(error).containsEntry("code", -32602).containsEntry("message", "Invalid parameters: the message has no messageId");
    }

    @Test
    @SuppressWarnings("unchecked")
    void aVersionTheInterfaceDoesNotSpeakIsRefused() throws Exception {
        answerAbout("PX-A2A-9104", "{\"reply\": \"ok\"}");
        Map<String, Object> error = (Map<String, Object>) json(send("POST", JSONRPC, rpc(1, "SendMessage", message("PX-A2A-9104")), "A2A-Version", "0.3").body()).get("error");
        assertThat(error).containsEntry("code", -32009);
        assertThat(send("POST", REST + "/message:send", message("PX-A2A-9104"), "A2A-Version", "2.0").statusCode()).isEqualTo(400);
        assertThat(send("POST", JSONRPC, rpc(2, "SendMessage", message("PX-A2A-9104")), "A2A-Version", "1.0").body()).contains("\"result\"");
    }

    @Test
    void aStreamingOnlyOperationOfAnAgentThatDoesNotStreamIsUnsupported(@TempDir Path dir) throws Exception {
        Path card = dir.resolve("agent-card.json");
        Files.writeString(card, Files.readString(Path.of(CARD)).replace("\"streaming\": true", "\"streaming\": false"));
        WireMockServer other = new WireMockServer(options().bindAddress("127.0.0.1").dynamicPort().extensions(new A2aExtensionFactory(new A2aSettings(card.toString()))));
        other.start();
        try {
            other.stubFor(com.github.tomakehurst.wiremock.client.WireMock.post("/a2a/messages")
                    .willReturn(com.github.tomakehurst.wiremock.client.WireMock.okJson("{\"reply\": \"ok\"}")));
            HttpResponse<String> res = HTTP.send(java.net.http.HttpRequest.newBuilder(java.net.URI.create("http://127.0.0.1:" + other.port() + JSONRPC))
                    .POST(java.net.http.HttpRequest.BodyPublishers.ofString(rpc(1, "SendStreamingMessage", message("hi")))).build(),
                    HttpResponse.BodyHandlers.ofString());
            assertThat(res.body()).contains("-32004");
        } finally {
            other.stop();
        }
    }

    @Test
    @SuppressWarnings("unchecked")
    void aResetForgetsTheTasksAndKeepsTheAgent() throws Exception {
        answerAbout("PX-A2A-9105", "{\"state\": \"completed\"}");
        Map<String, Object> result = (Map<String, Object>) json(send("POST", JSONRPC, rpc(1, "SendMessage", message("PX-A2A-9105"))).body()).get("result");
        String id = (String) ((Map<String, Object>) result.get("task")).get("id");
        assertThat(send("GET", REST + "/tasks/" + id, null).statusCode()).isEqualTo(200);
        wm.resetAll();
        assertThat(send("GET", REST + "/tasks/" + id, null).statusCode()).isEqualTo(404);
        assertThat(send("GET", "/.well-known/agent-card.json", null).statusCode()).isEqualTo(200);
    }

    @Test
    void aWrongStubIsRefusedWhenItIsAdded() {
        assertThatThrownBy(() -> answerAbout("x", "{\"text\": \"typo\"}")).hasMessageContaining("is not an A2A agent stub: an agent's answer has no key \"text\"");
        assertThatThrownBy(() -> answerAbout("x", "{\"state\": \"done\"}")).hasMessageContaining("a task's state is one of completed, input-required");
        assertThatThrownBy(() -> answerAbout("x", "{\"reply\": \"ok\", \"state\": \"completed\"}")).hasMessageContaining("not both");
        assertThatThrownBy(() -> answerAbout("x", "{\"state\": \"completed\", \"artifacts\": [{\"name\": \"a\", \"text\": \"t\", \"url\": \"u\"}]}"))
                .hasMessageContaining("the artifact a has one of text, data or url");
        assertThatThrownBy(() -> answerAbout("x", "{\"error\": {\"code\": \"busy\"}}")).hasMessageContaining("an agent's error is");
    }

    @Test
    void aWrongCardStopsWireMock(@TempDir Path dir) throws Exception {
        Path card = dir.resolve("agent-card.json");
        Files.writeString(card, "{\"name\": \"Partner carrier agent\", \"supportedInterfaces\": []}");
        assertThatThrownBy(() -> new WireMockServer(options().bindAddress("127.0.0.1").dynamicPort()
                .extensions(new A2aExtensionFactory(new A2aSettings(card.toString())))).start())
                .hasMessageContaining("the agent card " + card + " (A2A_AGENT_CARD_SOURCE) is wrong: it has no description");
        assertThat(A2aSettings.fromEnv(Map.of()).enabled()).isFalse();
        assertThat(A2aSettings.fromEnv(Map.of("A2A_AGENT_CARD_SOURCE", "/var/a2a/agent-card.json")).cardSource()).isEqualTo("/var/a2a/agent-card.json");
    }
}
