// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.anthropic.backends.Backend;
import com.anthropic.bedrock.backends.BedrockBackend;
import com.anthropic.client.AnthropicClient;
import com.anthropic.client.okhttp.AnthropicOkHttpClient;
import com.anthropic.core.JsonValue;
import com.anthropic.core.http.HttpRequest;
import com.anthropic.core.http.HttpResponse;
import com.anthropic.core.http.StreamResponse;
import com.anthropic.errors.AnthropicException;
import com.anthropic.errors.BadRequestException;
import com.anthropic.errors.InternalServerException;
import com.anthropic.errors.RateLimitException;
import com.anthropic.errors.UnauthorizedException;
import com.anthropic.helpers.MessageAccumulator;
import com.anthropic.models.messages.ContentBlock;
import com.anthropic.models.messages.ContentBlockParam;
import com.anthropic.models.messages.JsonOutputFormat;
import com.anthropic.models.messages.Message;
import com.anthropic.models.messages.MessageCreateParams;
import com.anthropic.models.messages.OutputConfig;
import com.anthropic.models.messages.RawMessageStreamEvent;
import com.anthropic.models.messages.StopReason;
import com.anthropic.models.messages.Tool;
import com.anthropic.models.messages.ToolResultBlockParam;
import com.anthropic.models.messages.ToolUseBlock;
import com.anthropic.vertex.backends.VertexBackend;
import com.google.auth.oauth2.AccessToken;
import com.google.auth.oauth2.GoogleCredentials;

import org.junit.jupiter.api.Test;

import software.amazon.awssdk.auth.credentials.AwsBasicCredentials;
import software.amazon.awssdk.regions.Region;

import java.time.Duration;
import java.util.ArrayList;
import java.util.Date;
import java.util.List;
import java.util.Map;

/**
 * Anthropic's Messages API, as the official Java SDK reads it: directly, on Bedrock and on Vertex
 * AI. Anthropic publishes no specification: its SDK is the contract.
 */
class AnthropicTest extends ModelMockTest {
    private static final String MODEL = "claude-sonnet-4-5";

    private AnthropicClient client() {
        return AnthropicOkHttpClient.builder().baseUrl(url()).apiKey("sk-ant-test").maxRetries(0).timeout(Duration.ofSeconds(10)).build();
    }

    private static MessageCreateParams.Builder ask(String question) {
        return MessageCreateParams.builder().model(MODEL).maxTokens(1024).system("You help recipients track their parcels.")
                .addUserMessage(question);
    }

    private static Tool trackParcel() {
        return Tool.builder().name("track_parcel").description("Finds where a parcel is.")
                .inputSchema(Tool.InputSchema.builder()
                        .properties(Tool.InputSchema.Properties.builder()
                                .putAdditionalProperty("reference", JsonValue.from(Map.of("type", "string"))).build())
                        .required(List.of("reference"))
                        .build())
                .build();
    }

    @Test
    void aMessageAnswersWithText() {
        answer("PX-AI-8201", "{\"text\": \"Your parcel PX-AI-8201 is out for delivery.\", \"reasoning\": \"The last scan says so.\"}");
        Message m = client().messages().create(ask("Where is my parcel PX-AI-8201?").build());
        m.validate();
        assertThat(m.content().get(0).thinking().get().thinking()).isEqualTo("The last scan says so.");
        assertThat(m.content().get(1).text().get().text()).isEqualTo("Your parcel PX-AI-8201 is out for delivery.");
        assertThat(m.stopReason()).contains(StopReason.END_TURN);
        assertThat(m.model().asString()).isEqualTo(MODEL);
        assertThat(m.usage().outputTokens()).isPositive();
    }

    @Test
    void aToolLoopIsWalkedTurnByTurn() {
        answer("PX-AI-8202", "{\"toolCalls\": [{\"name\": \"track_parcel\", \"arguments\": {\"reference\": \"PX-AI-8202\"}}]}");
        answerAfter("PX-AI-8202", "track_parcel", "{\"text\": \"PX-AI-8202 is out for delivery and arrives today.\"}");
        MessageCreateParams.Builder params = ask("Where is PX-AI-8202?").addTool(trackParcel());
        Message first = client().messages().create(params.build());
        first.validate();
        assertThat(first.stopReason()).contains(StopReason.TOOL_USE);
        ToolUseBlock use = first.content().get(0).toolUse().get();
        assertThat(use.name()).isEqualTo("track_parcel");
        assertThat(use._input()).isEqualTo(JsonValue.from(Map.of("reference", "PX-AI-8202")));

        Message second = client().messages().create(params.addMessage(first)
                .addUserMessageOfBlockParams(List.of(ContentBlockParam.ofToolResult(ToolResultBlockParam.builder()
                        .toolUseId(use.id()).content("{\"status\":\"OUT_FOR_DELIVERY\"}").build())))
                .build());
        second.validate();
        assertThat(second.content().get(0).text().get().text()).isEqualTo("PX-AI-8202 is out for delivery and arrives today.");
    }

    @Test
    void structuredOutputIsTheJsonText() {
        answer("Lindenweg 14", "{\"json\": {\"street\": \"Lindenweg 14\", \"postcode\": \"04109\"}}");
        Message m = client().messages().create(ask("Deliver to Lindenweg 14, 04109 Leipzig.")
                .outputConfig(OutputConfig.builder().format(JsonOutputFormat.builder()
                        .schema(JsonOutputFormat.Schema.builder().putAdditionalProperty("type", JsonValue.from("object")).build()).build()).build())
                .build());
        assertThat(m.content().get(0).text().get().text()).isEqualTo("{\"street\":\"Lindenweg 14\",\"postcode\":\"04109\"}");
    }

    @Test
    void refusalsAndEarlyStops() {
        answer("PX-AI-8203", "{\"refusal\": \"I can't share another recipient's address.\"}");
        answer("PX-AI-8204", "{\"text\": \"Your parcel is\", \"stop\": \"length\"}");
        Message refused = client().messages().create(ask("Who lives at the address of PX-AI-8203?").build());
        refused.validate();
        assertThat(refused.stopReason()).contains(StopReason.REFUSAL);
        assertThat(refused.content().get(0).text().get().text()).isEqualTo("I can't share another recipient's address.");
        assertThat(client().messages().create(ask("PX-AI-8204?").build()).stopReason()).contains(StopReason.MAX_TOKENS);
    }

    @Test
    void aStreamIsAccumulatedToTheWholeMessage() {
        answer("PX-AI-8205", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"reasoning\": \"It was scanned there.\", "
                + "\"toolCalls\": [{\"name\": \"notify_recipient\", \"arguments\": {\"reference\": \"PX-AI-8205\"}}]}");
        MessageAccumulator acc = MessageAccumulator.create();
        try (StreamResponse<RawMessageStreamEvent> s = client().messages().createStreaming(ask("Where is PX-AI-8205?").build())) {
            s.stream().forEach(acc::accumulate);
        }
        Message m = acc.message();
        m.validate();
        assertThat(m.content()).hasSize(3);
        assertThat(m.content().get(0).thinking().get().thinking()).isEqualTo("It was scanned there.");
        assertThat(m.content().get(0).thinking().get().signature()).isNotEmpty();
        assertThat(m.content().get(1).text().get().text()).isEqualTo("Your parcel is at the Leipzig depot.");
        assertThat(m.content().get(2).toolUse().get()._input()).isEqualTo(JsonValue.from(Map.of("reference", "PX-AI-8205")));
        assertThat(m.stopReason()).contains(StopReason.TOOL_USE);
        assertThat(last().getResponse().getBodyAsString()).contains("event: ping").endsWith("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n");
    }

    @Test
    void aStreamFailsInTheMiddleOrIsCutOff() {
        answer("PX-AI-8206", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"error\": {\"type\": \"overloaded\", \"afterEvents\": 4}}");
        answer("PX-AI-8207", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"cutOffAfter\": 4}");
        List<RawMessageStreamEvent> events = new ArrayList<>();
        assertThatThrownBy(() -> {
            try (StreamResponse<RawMessageStreamEvent> s = client().messages().createStreaming(ask("Where is PX-AI-8206?").build())) {
                s.stream().forEach(events::add);
            }
        }).isInstanceOf(AnthropicException.class).hasMessageContaining("Overloaded");
        // Four events on the wire, the ping among them, which the SDK does not pass on.
        assertThat(events).hasSize(3);
        events.clear();
        try (StreamResponse<RawMessageStreamEvent> s = client().messages().createStreaming(ask("Where is PX-AI-8207?").build())) {
            s.stream().forEach(events::add);
        }
        assertThat(events).hasSize(3).noneMatch(RawMessageStreamEvent::isMessageStop);
    }

    @Test
    void errorsAreAnthropicErrors() {
        answer("PX-AI-8210", "{\"error\": {\"type\": \"rate_limit\", \"retryAfter\": 3}}");
        answer("PX-AI-8211", "{\"error\": {\"type\": \"overloaded\"}}");
        answer("PX-AI-8212", "{\"error\": {\"type\": \"context_length\"}}");
        answer("PX-AI-8213", "{\"error\": {\"type\": \"auth\"}}");
        answer("PX-AI-8214", "{\"error\": {\"type\": \"server\"}}");
        assertThatThrownBy(() -> client().messages().create(ask("PX-AI-8210").build()))
                .isInstanceOfSatisfying(RateLimitException.class, e -> assertThat(e.headers().values("retry-after")).containsExactly("3"));
        assertThatThrownBy(() -> client().messages().create(ask("PX-AI-8211").build()))
                .isInstanceOfSatisfying(InternalServerException.class, e -> {
                    assertThat(e.statusCode()).isEqualTo(529);
                    assertThat(e.getMessage()).contains("overloaded_error");
                });
        assertThatThrownBy(() -> client().messages().create(ask("PX-AI-8212").build()))
                .isInstanceOfSatisfying(BadRequestException.class, e -> assertThat(e.getMessage()).contains("prompt is too long"));
        assertThatThrownBy(() -> client().messages().create(ask("PX-AI-8213").build())).isInstanceOf(UnauthorizedException.class);
        assertThatThrownBy(() -> client().messages().create(ask("PX-AI-8214").build()))
                .isInstanceOfSatisfying(InternalServerException.class, e -> assertThat(e.statusCode()).isEqualTo(500));
    }

    @Test
    void modelsAreTheStubs() {
        stub(mapping("{\"endpoint\": \"models\"}", "{\"models\": [\"claude-sonnet-4-5\", \"claude-haiku-4-5\"]}"));
        assertThat(client().models().list().data()).extracting(m -> m.id()).containsExactly("claude-sonnet-4-5", "claude-haiku-4-5");
    }

    /**
     * The SDK's Bedrock backend, at WireMock: it builds Bedrock's URL from the region and takes no
     * other, so the test gives it WireMock's and leaves the rest to it (signing the requests, and
     * reading AWS's event streams).
     */
    private AnthropicClient bedrock() {
        BedrockBackend bedrock = BedrockBackend.builder().awsCredentials(AwsBasicCredentials.create("AKIDPARCELS", "parcels-secret"))
                .region(Region.EU_CENTRAL_1).build();
        String base = url();
        return AnthropicOkHttpClient.builder().backend(new Backend() {
            @Override
            public String baseUrl() {
                return base;
            }

            @Override
            public HttpRequest prepareRequest(HttpRequest request) {
                return bedrock.prepareRequest(request);
            }

            @Override
            public HttpRequest authorizeRequest(HttpRequest request) {
                return bedrock.authorizeRequest(request);
            }

            @Override
            public HttpResponse prepareResponse(HttpResponse response) {
                return bedrock.prepareResponse(response);
            }

            @Override
            public void close() {
                bedrock.close();
            }
        }).maxRetries(0).build();
    }

    @Test
    void anthropicOnBedrockAnswersAndStreamsInAwsEventStreams() {
        answer("PX-AI-8220", "{\"text\": \"Your parcel is at the Leipzig depot.\", "
                + "\"toolCalls\": [{\"name\": \"notify_recipient\", \"arguments\": {\"reference\": \"PX-AI-8220\"}}]}");
        MessageCreateParams params = MessageCreateParams.builder().model("eu.anthropic.claude-sonnet-4-5-20250929-v1:0").maxTokens(1024)
                .addUserMessage("Where is PX-AI-8220?").build();
        Message m = bedrock().messages().create(params);
        m.validate();
        assertThat(last().getRequest().getUrl()).isEqualTo("/model/eu.anthropic.claude-sonnet-4-5-20250929-v1:0/invoke");
        assertThat(m.content().get(0).text().get().text()).isEqualTo("Your parcel is at the Leipzig depot.");

        MessageAccumulator acc = MessageAccumulator.create();
        try (StreamResponse<RawMessageStreamEvent> s = bedrock().messages().createStreaming(params)) {
            s.stream().forEach(acc::accumulate);
        }
        assertThat(last().getResponse().getHeaders().getContentTypeHeader().mimeTypePart()).isEqualTo("application/vnd.amazon.eventstream");
        assertThat(acc.message().content().get(0).text().get().text()).isEqualTo("Your parcel is at the Leipzig depot.");
        assertThat(acc.message().content().get(1).toolUse().get().name()).isEqualTo("notify_recipient");
    }

    @Test
    void anthropicOnBedrockFailsAsBedrock() {
        answer("PX-AI-8221", "{\"error\": {\"type\": \"rate_limit\"}}");
        assertThatThrownBy(() -> bedrock().messages().create(MessageCreateParams.builder().model("anthropic.claude-haiku-4-5-20251001-v1:0")
                .maxTokens(256).addUserMessage("PX-AI-8221").build()))
                .isInstanceOfSatisfying(RateLimitException.class, e -> assertThat(e.getMessage()).contains("Too many requests"));
    }

    @Test
    void anthropicOnVertexAnswersAndStreams() {
        answer("PX-AI-8222", "{\"text\": \"Your parcel is at the Leipzig depot.\"}");
        AnthropicClient vertex = AnthropicOkHttpClient.builder()
                .backend(VertexBackend.builder().region("europe-west1").project("parcels-prod").baseUrl(url())
                        .googleCredentials(GoogleCredentials.create(new AccessToken("ya29.parcels", new Date(System.currentTimeMillis() + 3_600_000))))
                        .build())
                .maxRetries(0).build();
        MessageCreateParams params = MessageCreateParams.builder().model("claude-sonnet-4-5@20250929").maxTokens(1024)
                .addUserMessage("Where is PX-AI-8222?").build();
        Message m = vertex.messages().create(params);
        m.validate();
        assertThat(last().getRequest().getUrl()).endsWith("/publishers/anthropic/models/claude-sonnet-4-5@20250929:rawPredict");
        assertThat(m.content().get(0).text().get().text()).isEqualTo("Your parcel is at the Leipzig depot.");
        MessageAccumulator acc = MessageAccumulator.create();
        try (StreamResponse<RawMessageStreamEvent> s = vertex.messages().createStreaming(params)) {
            s.stream().forEach(acc::accumulate);
        }
        assertThat(acc.message().content().get(0).text().map(b -> b.text())).contains("Your parcel is at the Leipzig depot.");
        assertThat(acc.message().content()).extracting(ContentBlock::isText).containsExactly(true);
    }
}
