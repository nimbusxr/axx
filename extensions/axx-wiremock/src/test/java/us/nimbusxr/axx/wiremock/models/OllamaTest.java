// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static org.assertj.core.api.Assertions.assertThat;

import com.github.tomakehurst.wiremock.common.Json;

import org.junit.jupiter.api.Test;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * Ollama's own API, which has no official Java SDK: its answers are checked against Ollama's
 * published OpenAPI document, and read as Ollama's clients read them.
 */
class OllamaTest extends ModelMockTest {
    private static final String SPEC = "ollama.yaml";
    private static final HttpClient HTTP = HttpClient.newBuilder().version(HttpClient.Version.HTTP_1_1).build();

    private HttpResponse<String> post(String path, String body) throws Exception {
        return HTTP.send(HttpRequest.newBuilder(URI.create(url() + path)).header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> json(String text) {
        return Json.read(text, Map.class);
    }

    private static List<Map<String, Object>> lines(String ndjson) {
        List<Map<String, Object>> out = new ArrayList<>();
        for (String line : ndjson.split("\n")) {
            out.add(json(line));
        }
        return out;
    }

    private static final String ASK = "{\"model\": \"llama3.2\", \"stream\": false, \"messages\": ["
            + "{\"role\": \"system\", \"content\": \"You help recipients track their parcels.\"}, "
            + "{\"role\": \"user\", \"content\": \"Where is PX-AI-8501?\"}]}";

    @Test
    @SuppressWarnings("unchecked")
    void chatAnswersWithoutAStreamWhenAsked() throws Exception {
        answer("PX-AI-8501", "{\"text\": \"Your parcel PX-AI-8501 is out for delivery.\", \"reasoning\": \"The last scan says so.\"}");
        HttpResponse<String> res = post("/api/chat", ASK);
        assertThat(res.statusCode()).isEqualTo(200);
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
        Map<String, Object> body = json(res.body());
        Map<String, Object> message = (Map<String, Object>) body.get("message");
        assertThat(message).containsEntry("role", "assistant").containsEntry("content", "Your parcel PX-AI-8501 is out for delivery.")
                .containsEntry("thinking", "The last scan says so.");
        assertThat(body).containsEntry("done", true).containsEntry("done_reason", "stop").containsEntry("model", "llama3.2");
        assertThat(((Number) body.get("eval_count")).intValue()).isPositive();
    }

    @Test
    @SuppressWarnings("unchecked")
    void chatStreamsNewlineDelimitedJsonByDefault() throws Exception {
        answer("PX-AI-8502", "{\"text\": \"Your parcel is at the Leipzig depot.\", "
                + "\"toolCalls\": [{\"name\": \"notify_recipient\", \"arguments\": {\"reference\": \"PX-AI-8502\"}}]}");
        HttpResponse<String> res = post("/api/chat", "{\"model\": \"llama3.2\", \"messages\": [{\"role\": \"user\", \"content\": \"Where is PX-AI-8502?\"}]}");
        assertThat(res.headers().firstValue("Content-Type")).contains("application/x-ndjson");
        StringBuilder text = new StringBuilder();
        List<Map<String, Object>> lines = lines(res.body());
        for (String line : res.body().split("\n")) {
            assertThat(Contracts.schema(SPEC, "ChatStreamEvent", line)).as(line).isEmpty();
        }
        for (Map<String, Object> line : lines) {
            text.append(((Map<String, Object>) line.get("message")).get("content"));
        }
        assertThat(text).hasToString("Your parcel is at the Leipzig depot.");
        Map<String, Object> calls = lines.get(lines.size() - 2);
        Map<String, Object> call = (Map<String, Object>) ((List<Object>) ((Map<String, Object>) calls.get("message")).get("tool_calls")).get(0);
        assertThat((Map<String, Object>) call.get("function")).containsEntry("name", "notify_recipient")
                .containsEntry("arguments", Map.of("reference", "PX-AI-8502"));
        assertThat(lines.get(lines.size() - 1)).containsEntry("done", true);
        assertThat(lines.subList(0, lines.size() - 1)).allSatisfy(l -> assertThat(l).containsEntry("done", false));
    }

    @Test
    @SuppressWarnings("unchecked")
    void aToolLoopIsWalkedByToolNameOrCallId() throws Exception {
        answer("PX-AI-8503", "{\"toolCalls\": [{\"name\": \"track_parcel\", \"arguments\": {\"reference\": \"PX-AI-8503\"}}]}");
        answerAfter("PX-AI-8503", "track_parcel", "{\"text\": \"PX-AI-8503 is out for delivery.\"}");
        String q = "{\"role\": \"user\", \"content\": \"Where is PX-AI-8503?\"}";
        Map<String, Object> first = json(post("/api/chat", "{\"model\": \"llama3.2\", \"stream\": false, \"messages\": [" + q + "]}").body());
        Map<String, Object> assistant = (Map<String, Object>) first.get("message");
        String id = (String) ((Map<String, Object>) ((List<Object>) assistant.get("tool_calls")).get(0)).get("id");
        Map<String, Object> byName = json(post("/api/chat", "{\"model\": \"llama3.2\", \"stream\": false, \"messages\": [" + q + ", "
                + Json.write(assistant) + ", {\"role\": \"tool\", \"tool_name\": \"track_parcel\", \"content\": \"{\\\"status\\\":\\\"OUT_FOR_DELIVERY\\\"}\"}]}").body());
        assertThat((Map<String, Object>) byName.get("message")).containsEntry("content", "PX-AI-8503 is out for delivery.");
        Map<String, Object> byId = json(post("/api/chat", "{\"model\": \"llama3.2\", \"stream\": false, \"messages\": [" + q + ", "
                + Json.write(assistant) + ", {\"role\": \"tool\", \"tool_call_id\": \"" + id + "\", \"content\": \"OUT_FOR_DELIVERY\"}]}").body());
        assertThat((Map<String, Object>) byId.get("message")).containsEntry("content", "PX-AI-8503 is out for delivery.");
    }

    @Test
    void generateAnswersAndStreams() throws Exception {
        answer("PX-AI-8504", "{\"text\": \"Out for delivery.\", \"stop\": \"length\"}");
        HttpResponse<String> one = post("/api/generate", "{\"model\": \"llama3.2\", \"prompt\": \"Where is PX-AI-8504?\", \"stream\": false}");
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
        assertThat(json(one.body())).containsEntry("response", "Out for delivery.").containsEntry("done_reason", "length");
        HttpResponse<String> stream = post("/api/generate", "{\"model\": \"llama3.2\", \"prompt\": \"Where is PX-AI-8504?\"}");
        StringBuilder text = new StringBuilder();
        for (String line : stream.body().split("\n")) {
            assertThat(Contracts.schema(SPEC, "GenerateStreamEvent", line)).as(line).isEmpty();
            text.append(json(line).get("response"));
        }
        assertThat(text).hasToString("Out for delivery.");
    }

    @Test
    void errorsAreOllamas() throws Exception {
        answer("PX-AI-8510", "{\"error\": {\"type\": \"overloaded\"}}");
        answer("PX-AI-8511", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"error\": {\"type\": \"server\", \"afterEvents\": 2}}");
        HttpResponse<String> busy = post("/api/chat", ASK.replace("PX-AI-8501", "PX-AI-8510"));
        assertThat(busy.statusCode()).isEqualTo(503);
        assertThat(json(busy.body())).containsEntry("error", "server busy, please try again.  maximum pending requests exceeded");
        assertThat(Contracts.schema(SPEC, "ErrorResponse", busy.body())).isEmpty();
        HttpResponse<String> broken = post("/api/chat", "{\"model\": \"llama3.2\", \"messages\": [{\"role\": \"user\", \"content\": \"PX-AI-8511\"}]}");
        List<Map<String, Object>> lines = lines(broken.body());
        assertThat(lines).hasSize(3);
        assertThat((String) lines.get(2).get("error")).startsWith("model runner has unexpectedly stopped");
    }

    @Test
    @SuppressWarnings("unchecked")
    void embeddingsAndTags() throws Exception {
        stub(mapping("{\"endpoint\": \"embeddings\"}", "{}"));
        stub(mapping("{\"endpoint\": \"models\"}", "{\"models\": [\"llama3.2\", \"nomic-embed-text:v1.5\"]}"));
        HttpResponse<String> embed = post("/api/embed", "{\"model\": \"nomic-embed-text\", \"input\": [\"Lindenweg 14\", \"Hauptstraße 5\"]}");
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
        List<List<Number>> vectors = (List<List<Number>>) json(embed.body()).get("embeddings");
        assertThat(vectors).hasSize(2);
        assertThat(vectors.get(1).stream().map(Number::floatValue).toList()).isEqualTo(Wire.vector("Hauptstraße 5", 768));
        HttpResponse<String> legacy = post("/api/embeddings", "{\"model\": \"nomic-embed-text\", \"prompt\": \"Lindenweg 14\"}");
        assertThat(((List<Number>) json(legacy.body()).get("embedding")).stream().map(Number::floatValue).toList())
                .isEqualTo(Wire.vector("Lindenweg 14", 768));
        HttpResponse<String> tags = HTTP.send(HttpRequest.newBuilder(URI.create(url() + "/api/tags")).build(), HttpResponse.BodyHandlers.ofString());
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
        assertThat((List<Map<String, Object>>) json(tags.body()).get("models")).extracting(m -> m.get("name"))
                .containsExactly("llama3.2:latest", "nomic-embed-text:v1.5");
    }
}
