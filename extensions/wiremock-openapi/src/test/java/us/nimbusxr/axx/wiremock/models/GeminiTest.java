// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.google.auth.oauth2.AccessToken;
import com.google.auth.oauth2.GoogleCredentials;
import com.google.genai.Client;
import com.google.genai.ResponseStream;
import com.google.genai.errors.ApiException;
import com.google.genai.errors.ClientException;
import com.google.genai.errors.ServerException;
import com.google.genai.types.Content;
import com.google.genai.types.EmbedContentConfig;
import com.google.genai.types.EmbedContentResponse;
import com.google.genai.types.FinishReason;
import com.google.genai.types.FunctionCall;
import com.google.genai.types.FunctionDeclaration;
import com.google.genai.types.GenerateContentConfig;
import com.google.genai.types.GenerateContentResponse;
import com.google.genai.types.HttpOptions;
import com.google.genai.types.HttpRetryOptions;
import com.google.genai.types.ListModelsConfig;
import com.google.genai.types.Model;
import com.google.genai.types.Part;
import com.google.genai.types.Schema;
import com.google.genai.types.Tool;
import com.google.genai.types.Type;

import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.Date;
import java.util.List;
import java.util.Map;

/** Gemini's API, on AI Studio and on Vertex AI, as Google's official Java SDK (google-genai) reads it. */
class GeminiTest extends ModelMockTest {
    private static final String MODEL = "gemini-2.5-flash";

    private HttpOptions http() {
        return HttpOptions.builder().baseUrl(url()).retryOptions(HttpRetryOptions.builder().attempts(1).build()).build();
    }

    private Client client() {
        return Client.builder().apiKey("parcels-key").httpOptions(http()).build();
    }

    private static GenerateContentConfig.Builder config() {
        return GenerateContentConfig.builder().systemInstruction(Content.fromParts(Part.fromText("You help recipients track their parcels.")));
    }

    private static Tool trackParcel() {
        return Tool.builder().functionDeclarations(FunctionDeclaration.builder().name("track_parcel").description("Finds where a parcel is.")
                .parameters(Schema.builder().type(Type.Known.OBJECT).properties(Map.of("reference", Schema.builder().type(Type.Known.STRING).build())))
                .build()).build();
    }

    @Test
    void generateContentAnswersWithText() {
        answer("PX-AI-8301", "{\"text\": \"Your parcel PX-AI-8301 is out for delivery.\", \"reasoning\": \"The last scan says so.\"}");
        GenerateContentResponse r = client().models.generateContent(MODEL, "Where is my parcel PX-AI-8301?", config().build());
        assertThat(last().getRequest().getUrl()).isEqualTo("/v1beta/models/gemini-2.5-flash:generateContent");
        assertThat(r.text()).isEqualTo("Your parcel PX-AI-8301 is out for delivery.");
        assertThat(r.candidates().get().get(0).content().get().parts().get().get(0).thought()).contains(true);
        assertThat(r.finishReason().knownEnum()).isEqualTo(FinishReason.Known.STOP);
        assertThat(r.usageMetadata().get().thoughtsTokenCount().get()).isPositive();
        assertThat(r.modelVersion()).contains(MODEL);
    }

    @Test
    void aToolLoopIsWalkedTurnByTurn() {
        answer("PX-AI-8302", "{\"toolCalls\": [{\"name\": \"track_parcel\", \"arguments\": {\"reference\": \"PX-AI-8302\"}}]}");
        answerAfter("PX-AI-8302", "track_parcel", "{\"text\": \"PX-AI-8302 is out for delivery and arrives today.\"}");
        GenerateContentConfig config = config().tools(trackParcel()).build();
        Content question = Content.builder().role("user").parts(Part.fromText("Where is PX-AI-8302?")).build();
        GenerateContentResponse first = client().models.generateContent(MODEL, List.of(question), config);
        FunctionCall call = first.functionCalls().get(0);
        assertThat(call.name()).contains("track_parcel");
        assertThat(call.args()).contains(Map.of("reference", "PX-AI-8302"));
        GenerateContentResponse second = client().models.generateContent(MODEL, List.of(question,
                first.candidates().get().get(0).content().get(),
                Content.builder().role("user").parts(Part.fromFunctionResponse("track_parcel", Map.of("status", "OUT_FOR_DELIVERY"))).build()),
                config);
        assertThat(second.text()).isEqualTo("PX-AI-8302 is out for delivery and arrives today.");
    }

    @Test
    void structuredOutputAndEarlyStops() {
        answer("Lindenweg 14", "{\"json\": {\"street\": \"Lindenweg 14\", \"postcode\": \"04109\"}}");
        answer("PX-AI-8303", "{\"text\": \"Your parcel is\", \"stop\": \"length\"}");
        answer("PX-AI-8304", "{\"stop\": \"safety\"}");
        GenerateContentResponse r = client().models.generateContent(MODEL, "Deliver to Lindenweg 14, 04109 Leipzig.",
                config().responseMimeType("application/json").responseJsonSchema(Map.of("type", "object")).build());
        assertThat(r.text()).isEqualTo("{\"street\":\"Lindenweg 14\",\"postcode\":\"04109\"}");
        assertThat(client().models.generateContent(MODEL, "PX-AI-8303?", config().build()).finishReason().knownEnum())
                .isEqualTo(FinishReason.Known.MAX_TOKENS);
        GenerateContentResponse blocked = client().models.generateContent(MODEL, "PX-AI-8304?", config().build());
        assertThat(blocked.finishReason().knownEnum()).isEqualTo(FinishReason.Known.SAFETY);
        assertThat(blocked.candidates().get().get(0).safetyRatings().get().get(0).blocked()).contains(true);
    }

    @Test
    void aStreamIsServerSentEvents() {
        answer("PX-AI-8305", "{\"text\": \"Your parcel is at the Leipzig depot.\", "
                + "\"toolCalls\": [{\"name\": \"notify_recipient\", \"arguments\": {\"reference\": \"PX-AI-8305\"}}]}");
        StringBuilder text = new StringBuilder();
        List<GenerateContentResponse> chunks = new ArrayList<>();
        try (ResponseStream<GenerateContentResponse> s = client().models.generateContentStream(MODEL, "Where is PX-AI-8305?", config().build())) {
            for (GenerateContentResponse chunk : s) {
                chunks.add(chunk);
                if (chunk.functionCalls() == null || chunk.functionCalls().isEmpty()) {
                    text.append(chunk.text());
                }
            }
        }
        assertThat(last().getRequest().getUrl()).isEqualTo("/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse");
        assertThat(text).hasToString("Your parcel is at the Leipzig depot.");
        GenerateContentResponse end = chunks.get(chunks.size() - 1);
        assertThat(end.functionCalls().get(0).name()).contains("notify_recipient");
        assertThat(end.finishReason().knownEnum()).isEqualTo(FinishReason.Known.STOP);
        assertThat(end.usageMetadata().get().candidatesTokenCount().get()).isPositive();
    }

    @Test
    void aStreamFailsInTheMiddle() {
        answer("PX-AI-8306", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"error\": {\"type\": \"overloaded\", \"afterEvents\": 2}}");
        List<GenerateContentResponse> chunks = new ArrayList<>();
        assertThatThrownBy(() -> {
            try (ResponseStream<GenerateContentResponse> s = client().models.generateContentStream(MODEL, "Where is PX-AI-8306?", config().build())) {
                s.forEach(chunks::add);
            }
        }).isInstanceOfSatisfying(ServerException.class, e -> assertThat(e.code()).isEqualTo(503));
        assertThat(chunks).hasSize(2);
    }

    @Test
    void errorsAreGoogleErrors() {
        answer("PX-AI-8310", "{\"error\": {\"type\": \"rate_limit\", \"retryAfter\": 7}}");
        answer("PX-AI-8311", "{\"error\": {\"type\": \"overloaded\"}}");
        answer("PX-AI-8312", "{\"error\": {\"type\": \"context_length\"}}");
        answer("PX-AI-8313", "{\"error\": {\"type\": \"auth\"}}");
        answer("PX-AI-8314", "{\"error\": {\"type\": \"server\"}}");
        assertThatThrownBy(() -> client().models.generateContent(MODEL, "PX-AI-8310", config().build()))
                .isInstanceOfSatisfying(ClientException.class, e -> {
                    assertThat(e.code()).isEqualTo(429);
                    assertThat(e.status()).isEqualTo("RESOURCE_EXHAUSTED");
                });
        assertThat(last().getResponse().getBodyAsString()).contains("\"retryDelay\":\"7s\"");
        assertThatThrownBy(() -> client().models.generateContent(MODEL, "PX-AI-8311", config().build()))
                .isInstanceOfSatisfying(ServerException.class, e -> assertThat(e.status()).isEqualTo("UNAVAILABLE"));
        assertThatThrownBy(() -> client().models.generateContent(MODEL, "PX-AI-8312", config().build()))
                .isInstanceOfSatisfying(ClientException.class, e -> assertThat(e.status()).isEqualTo("INVALID_ARGUMENT"));
        assertThatThrownBy(() -> client().models.generateContent(MODEL, "PX-AI-8313", config().build()))
                .isInstanceOfSatisfying(ClientException.class, e -> assertThat(e.message()).contains("API key not valid"));
        assertThatThrownBy(() -> client().models.generateContent(MODEL, "PX-AI-8314", config().build()))
                .isInstanceOfSatisfying(ServerException.class, e -> assertThat(e.code()).isEqualTo(500));
    }

    @Test
    void embeddingsAndModels() {
        stub(mapping("{\"endpoint\": \"embeddings\"}", "{}"));
        stub(mapping("{\"endpoint\": \"models\"}", "{\"models\": [\"gemini-2.5-flash\", \"gemini-embedding-001\"]}"));
        EmbedContentResponse one = client().models.embedContent("gemini-embedding-001", "Lindenweg 14, 04109 Leipzig",
                EmbedContentConfig.builder().outputDimensionality(8).build());
        assertThat(one.embeddings().get().get(0).values().get()).isEqualTo(Wire.vector("Lindenweg 14, 04109 Leipzig", 8));
        EmbedContentResponse many = client().models.embedContent("gemini-embedding-001", List.of("Lindenweg 14", "Hauptstraße 5"),
                EmbedContentConfig.builder().build());
        assertThat(many.embeddings().get()).hasSize(2);
        assertThat(many.embeddings().get().get(1).values().get()).hasSize(3072).isEqualTo(Wire.vector("Hauptstraße 5", 3072));
        List<String> names = new ArrayList<>();
        for (Model m : client().models.list(ListModelsConfig.builder().build())) {
            names.add(m.name().get());
        }
        assertThat(names).containsExactly("models/gemini-2.5-flash", "models/gemini-embedding-001");
    }

    @Test
    void vertexAiAnswersOnItsPaths() {
        answer("PX-AI-8320", "{\"text\": \"Your parcel is at the Leipzig depot.\"}");
        answer("PX-AI-8321", "{\"error\": {\"type\": \"auth\"}}");
        Client vertex = Client.builder().vertexAI(true).project("parcels-prod").location("europe-west1")
                .credentials(GoogleCredentials.create(new AccessToken("ya29.parcels", new Date(System.currentTimeMillis() + 3_600_000))))
                .httpOptions(http()).build();
        GenerateContentResponse r = vertex.models.generateContent(MODEL, "Where is PX-AI-8320?", config().build());
        assertThat(last().getRequest().getUrl()).endsWith("/projects/parcels-prod/locations/europe-west1/publishers/google/models/gemini-2.5-flash:generateContent");
        assertThat(r.text()).isEqualTo("Your parcel is at the Leipzig depot.");
        assertThatThrownBy(() -> vertex.models.generateContent(MODEL, "PX-AI-8321", config().build()))
                .isInstanceOfSatisfying(ApiException.class, e -> assertThat(e.status()).isEqualTo("UNAUTHENTICATED"));
    }
}
