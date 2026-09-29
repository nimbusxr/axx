// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.openai.client.OpenAIClient;
import com.openai.client.okhttp.OpenAIOkHttpClient;
import com.openai.core.JsonValue;
import com.openai.core.http.StreamResponse;
import com.openai.errors.BadRequestException;
import com.openai.errors.InternalServerException;
import com.openai.errors.OpenAIException;
import com.openai.errors.RateLimitException;
import com.openai.errors.UnauthorizedException;
import com.openai.helpers.ChatCompletionAccumulator;
import com.openai.helpers.ResponseAccumulator;
import com.openai.models.FunctionDefinition;
import com.openai.models.FunctionParameters;
import com.openai.models.ResponseFormatJsonSchema;
import com.openai.models.chat.completions.ChatCompletion;
import com.openai.models.chat.completions.ChatCompletionChunk;
import com.openai.models.chat.completions.ChatCompletionCreateParams;
import com.openai.models.chat.completions.ChatCompletionFunctionTool;
import com.openai.models.chat.completions.ChatCompletionMessageFunctionToolCall;
import com.openai.models.chat.completions.ChatCompletionStreamOptions;
import com.openai.models.chat.completions.ChatCompletionToolMessageParam;
import com.openai.models.embeddings.CreateEmbeddingResponse;
import com.openai.models.embeddings.EmbeddingCreateParams;
import com.openai.models.responses.FunctionTool;
import com.openai.models.responses.Response;
import com.openai.models.responses.ResponseCreateParams;
import com.openai.models.responses.ResponseFunctionToolCall;
import com.openai.models.responses.ResponseInputItem;
import com.openai.models.responses.ResponseStreamEvent;

import org.junit.jupiter.api.Test;

import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * OpenAI's Chat Completions and Responses APIs, as the official Java SDK reads them, checked
 * against OpenAI's published OpenAPI document.
 */
class OpenAiTest extends ModelMockTest {
    private static final String SPEC = "openai.json";
    private static final String MODEL = "gpt-4.1-mini";

    private OpenAIClient client() {
        return OpenAIOkHttpClient.builder().baseUrl(url() + "/v1").apiKey("sk-test").maxRetries(0).timeout(Duration.ofSeconds(10)).build();
    }

    private static ChatCompletionCreateParams.Builder ask(String question) {
        return ChatCompletionCreateParams.builder().model(MODEL).addSystemMessage("You help recipients track their parcels.")
                .addUserMessage(question);
    }

    private static ChatCompletionFunctionTool trackParcel() {
        return ChatCompletionFunctionTool.builder()
                .function(FunctionDefinition.builder().name("track_parcel").description("Finds where a parcel is.")
                        .parameters(FunctionParameters.builder()
                                .putAdditionalProperty("type", JsonValue.from("object"))
                                .putAdditionalProperty("properties", JsonValue.from(Map.of("reference", Map.of("type", "string"))))
                                .putAdditionalProperty("required", JsonValue.from(List.of("reference")))
                                .build())
                        .build())
                .build();
    }

    @Test
    void aChatCompletionAnswersWithText() {
        answer("PX-AI-8101", "{\"text\": \"Your parcel PX-AI-8101 is out for delivery.\"}");
        ChatCompletion c = client().chat().completions().create(ask("Where is my parcel PX-AI-8101?").build());
        c.validate();
        assertThat(c.choices().get(0).message().content()).contains("Your parcel PX-AI-8101 is out for delivery.");
        assertThat(c.choices().get(0).finishReason()).isEqualTo(ChatCompletion.Choice.FinishReason.STOP);
        assertThat(c.model()).isEqualTo(MODEL);
        assertThat(c.usage().get().totalTokens()).isPositive();
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
    }

    @Test
    void aToolLoopIsWalkedTurnByTurn() {
        answer("PX-AI-8102", "{\"toolCalls\": [{\"name\": \"track_parcel\", \"arguments\": {\"reference\": \"PX-AI-8102\"}}]}");
        answerAfter("PX-AI-8102", "track_parcel", "{\"text\": \"PX-AI-8102 is out for delivery and arrives today.\"}");
        ChatCompletionCreateParams.Builder params = ask("Where is PX-AI-8102?").addTool(trackParcel());
        ChatCompletion first = client().chat().completions().create(params.build());
        first.validate();
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
        assertThat(first.choices().get(0).finishReason()).isEqualTo(ChatCompletion.Choice.FinishReason.TOOL_CALLS);
        ChatCompletionMessageFunctionToolCall call = first.choices().get(0).message().toolCalls().get().get(0).asFunction();
        assertThat(call.function().name()).isEqualTo("track_parcel");
        assertThat(call.function().arguments()).isEqualTo("{\"reference\":\"PX-AI-8102\"}");

        ChatCompletion second = client().chat().completions().create(params
                .addMessage(first.choices().get(0).message())
                .addMessage(ChatCompletionToolMessageParam.builder().toolCallId(call.id()).content("{\"status\":\"OUT_FOR_DELIVERY\"}").build())
                .build());
        assertThat(second.choices().get(0).message().content()).contains("PX-AI-8102 is out for delivery and arrives today.");
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
    }

    @Test
    void structuredOutputIsTheJsonText() {
        answer("Lindenweg 14", "{\"json\": {\"street\": \"Lindenweg 14\", \"postcode\": \"04109\", \"city\": \"Leipzig\"}}");
        ChatCompletion c = client().chat().completions().create(ask("Deliver to Lindenweg 14, 04109 Leipzig, ring twice.")
                .responseFormat(ResponseFormatJsonSchema.builder().jsonSchema(ResponseFormatJsonSchema.JsonSchema.builder().name("address")
                        .schema(ResponseFormatJsonSchema.JsonSchema.Schema.builder().putAdditionalProperty("type", JsonValue.from("object")).build())
                        .build()).build())
                .build());
        assertThat(c.choices().get(0).message().content()).contains("{\"street\":\"Lindenweg 14\",\"postcode\":\"04109\",\"city\":\"Leipzig\"}");
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
    }

    @Test
    void refusalsAndEarlyStops() {
        answer("PX-AI-8103", "{\"refusal\": \"I can't share another recipient's address.\"}");
        answer("PX-AI-8104", "{\"text\": \"Your parcel is\", \"stop\": \"length\"}");
        answer("PX-AI-8105", "{\"text\": \"\", \"stop\": \"safety\"}");
        ChatCompletion refused = client().chat().completions().create(ask("Who lives at the address of PX-AI-8103?").build());
        refused.validate();
        assertThat(refused.choices().get(0).message().refusal()).contains("I can't share another recipient's address.");
        assertThat(refused.choices().get(0).message().content()).isEmpty();
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
        assertThat(client().chat().completions().create(ask("PX-AI-8104?").build()).choices().get(0).finishReason())
                .isEqualTo(ChatCompletion.Choice.FinishReason.LENGTH);
        assertThat(client().chat().completions().create(ask("PX-AI-8105?").build()).choices().get(0).finishReason())
                .isEqualTo(ChatCompletion.Choice.FinishReason.CONTENT_FILTER);
    }

    @Test
    void aStreamIsAccumulatedToTheWholeAnswer() {
        answer("PX-AI-8106", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"reasoning\": \"The parcel was scanned in Leipzig.\", "
                + "\"toolCalls\": [{\"name\": \"notify_recipient\", \"arguments\": {\"reference\": \"PX-AI-8106\"}}]}");
        ChatCompletionAccumulator acc = ChatCompletionAccumulator.create();
        try (StreamResponse<ChatCompletionChunk> s = client().chat().completions().createStreaming(ask("Where is PX-AI-8106?")
                .streamOptions(ChatCompletionStreamOptions.builder().includeUsage(true).build()).build())) {
            s.stream().forEach(acc::accumulate);
        }
        ChatCompletion c = acc.chatCompletion();
        assertThat(c.choices().get(0).message().content()).contains("Your parcel is at the Leipzig depot.");
        ChatCompletionMessageFunctionToolCall call = c.choices().get(0).message().toolCalls().get().get(0).asFunction();
        assertThat(call.function().name()).isEqualTo("notify_recipient");
        assertThat(call.function().arguments()).isEqualTo("{\"reference\":\"PX-AI-8106\"}");
        assertThat(c.choices().get(0).finishReason()).isEqualTo(ChatCompletion.Choice.FinishReason.TOOL_CALLS);
        assertThat(c.usage()).isPresent();
        String body = last().getResponse().getBodyAsString();
        assertThat(body).endsWith("data: [DONE]\n\n");
        for (String chunk : Contracts.sseData(body)) {
            // The reasoning is not OpenAI's: it is how the OpenAI-compatible servers that show it
            // send it (DeepSeek, vLLM, OpenRouter, Ollama).
            assertThat(Contracts.schema(SPEC, "CreateChatCompletionStreamResponse", chunk))
                    .as(chunk).allMatch(f -> f.contains("property 'reasoning_content' is not defined") || f.contains("property 'reasoning' is not defined"));
        }
    }

    @Test
    void aStreamCutOffEndsWithoutItsEnd() {
        answer("PX-AI-8107", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"cutOffAfter\": 3}");
        List<ChatCompletionChunk> chunks = new ArrayList<>();
        try (StreamResponse<ChatCompletionChunk> s = client().chat().completions().createStreaming(ask("Where is PX-AI-8107?").build())) {
            s.stream().forEach(chunks::add);
        }
        assertThat(chunks).hasSize(3);
        assertThat(chunks).allSatisfy(ch -> assertThat(ch.choices().get(0).finishReason()).isEmpty());
        assertThat(last().getResponse().getBodyAsString()).doesNotContain("[DONE]");
    }

    @Test
    void aStreamFailsInTheMiddle() {
        answer("PX-AI-8108", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"error\": {\"type\": \"overloaded\", \"afterEvents\": 2}}");
        List<ChatCompletionChunk> chunks = new ArrayList<>();
        assertThatThrownBy(() -> {
            try (StreamResponse<ChatCompletionChunk> s = client().chat().completions().createStreaming(ask("Where is PX-AI-8108?").build())) {
                s.stream().forEach(chunks::add);
            }
        }).isInstanceOf(OpenAIException.class).hasMessageContaining("The server is overloaded or not ready yet.");
        assertThat(chunks).hasSize(2);
    }

    @Test
    void errorsAreOpenAiErrors() {
        answer("PX-AI-8110", "{\"error\": {\"type\": \"rate_limit\", \"retryAfter\": 2}}");
        answer("PX-AI-8111", "{\"error\": {\"type\": \"overloaded\"}}");
        answer("PX-AI-8112", "{\"error\": {\"type\": \"context_length\"}}");
        answer("PX-AI-8113", "{\"error\": {\"type\": \"auth\"}}");
        answer("PX-AI-8114", "{\"error\": {\"type\": \"server\", \"message\": \"The depot model is down.\"}}");
        assertThatThrownBy(() -> client().chat().completions().create(ask("PX-AI-8110").build()))
                .isInstanceOfSatisfying(RateLimitException.class, e -> {
                    assertThat(e.headers().values("retry-after")).containsExactly("2");
                    assertThat(e.code()).contains("rate_limit_exceeded");
                });
        assertThatThrownBy(() -> client().chat().completions().create(ask("PX-AI-8111").build()))
                .isInstanceOfSatisfying(InternalServerException.class, e -> assertThat(e.statusCode()).isEqualTo(503));
        assertThatThrownBy(() -> client().chat().completions().create(ask("PX-AI-8112").build()))
                .isInstanceOfSatisfying(BadRequestException.class, e -> assertThat(e.code()).contains("context_length_exceeded"));
        assertThatThrownBy(() -> client().chat().completions().create(ask("PX-AI-8113").build()))
                .isInstanceOf(UnauthorizedException.class);
        assertThatThrownBy(() -> client().chat().completions().create(ask("PX-AI-8114").build()))
                .isInstanceOfSatisfying(InternalServerException.class, e -> assertThat(e.getMessage()).contains("The depot model is down."));
        for (int i = 0; i < 5; i++) {
            assertThat(Contracts.schema(SPEC, "ErrorResponse", served().get(i).getResponse().getBodyAsString())).isEmpty();
        }
    }

    @Test
    void theSdkRetriesARateLimitAfterItsRetryAfter() {
        answer("PX-AI-8115", "{\"error\": {\"type\": \"rate_limit\", \"retryAfter\": 1}}");
        OpenAIClient retrying = OpenAIOkHttpClient.builder().baseUrl(url() + "/v1").apiKey("sk-test").maxRetries(1).build();
        long start = System.nanoTime();
        assertThatThrownBy(() -> retrying.chat().completions().create(ask("PX-AI-8115").build())).isInstanceOf(RateLimitException.class);
        assertThat(Duration.ofNanos(System.nanoTime() - start)).isGreaterThanOrEqualTo(Duration.ofMillis(900));
        assertThat(served()).hasSize(2);
    }

    @Test
    void malformedAnswersBreakOff() {
        answer("PX-AI-8116", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"malformed\": true}");
        assertThatThrownBy(() -> client().chat().completions().create(ask("PX-AI-8116").build())).isInstanceOf(OpenAIException.class);
    }

    @Test
    void delaysAndSlowStreamsAreWireMocks() {
        stub("{\"request\": {\"customMatcher\": {\"name\": \"model-request\", \"parameters\": {\"about\": \"PX-AI-8117\"}}}, "
                + "\"response\": {\"transformers\": [\"model-answer\"], \"jsonBody\": {\"text\": \"On its way.\"}, \"fixedDelayMilliseconds\": 400}}");
        stub("{\"request\": {\"customMatcher\": {\"name\": \"model-request\", \"parameters\": {\"about\": \"PX-AI-8118\"}}}, "
                + "\"response\": {\"transformers\": [\"model-answer\"], \"jsonBody\": {\"text\": \"On its way to you.\"}, "
                + "\"chunkedDribbleDelay\": {\"numberOfChunks\": 4, \"totalDuration\": 400}}}");
        long start = System.nanoTime();
        client().chat().completions().create(ask("PX-AI-8117").build());
        assertThat(Duration.ofNanos(System.nanoTime() - start)).isGreaterThanOrEqualTo(Duration.ofMillis(400));
        start = System.nanoTime();
        ChatCompletionAccumulator acc = ChatCompletionAccumulator.create();
        try (StreamResponse<ChatCompletionChunk> s = client().chat().completions().createStreaming(ask("PX-AI-8118").build())) {
            s.stream().forEach(acc::accumulate);
        }
        assertThat(Duration.ofNanos(System.nanoTime() - start)).isGreaterThanOrEqualTo(Duration.ofMillis(300));
        assertThat(acc.chatCompletion().choices().get(0).message().content()).contains("On its way to you.");
    }

    @Test
    void embeddingsAreTheSameVectorForTheSameText() {
        stub(mapping("{\"endpoint\": \"embeddings\"}", "{}"));
        CreateEmbeddingResponse a = client().embeddings().create(EmbeddingCreateParams.builder().model("text-embedding-3-small")
                .input("Lindenweg 14, 04109 Leipzig").dimensions(8).build());
        a.validate();
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
        CreateEmbeddingResponse b = client().embeddings().create(EmbeddingCreateParams.builder().model("text-embedding-3-small")
                .input("Lindenweg 14, 04109 Leipzig").dimensions(8).encodingFormat(EmbeddingCreateParams.EncodingFormat.FLOAT).build());
        assertThat(a.data().get(0).embedding()).hasSize(8).isEqualTo(b.data().get(0).embedding());
        assertThat(a.data().get(0).embedding()).isEqualTo(Wire.vector("Lindenweg 14, 04109 Leipzig", 8));
        double norm = a.data().get(0).embedding().stream().mapToDouble(f -> f * f).sum();
        assertThat(norm).isCloseTo(1.0, org.assertj.core.data.Offset.offset(1e-5));
        CreateEmbeddingResponse full = client().embeddings().create(EmbeddingCreateParams.builder().model("text-embedding-3-small")
                .input("Lindenweg 14").build());
        assertThat(full.data().get(0).embedding()).hasSize(1536);
    }

    @Test
    void modelsAreTheStubs() {
        stub(mapping("{\"endpoint\": \"models\"}", "{\"models\": [\"gpt-4.1-mini\", \"text-embedding-3-small\"]}"));
        assertThat(client().models().list().data()).extracting(m -> m.id()).containsExactly("gpt-4.1-mini", "text-embedding-3-small");
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
    }

    private static FunctionTool trackParcelTool() {
        return FunctionTool.builder().name("track_parcel").strict(false)
                .parameters(FunctionTool.Parameters.builder()
                        .putAdditionalProperty("type", JsonValue.from("object"))
                        .putAdditionalProperty("properties", JsonValue.from(Map.of("reference", Map.of("type", "string"))))
                        .build())
                .build();
    }

    @Test
    void aResponseAnswersAndContinuesByItsId() {
        answer("PX-AI-8120", "{\"toolCalls\": [{\"name\": \"track_parcel\", \"arguments\": {\"reference\": \"PX-AI-8120\"}}]}");
        answerAfter("PX-AI-8120", "track_parcel", "{\"text\": \"PX-AI-8120 is out for delivery.\", \"reasoning\": \"The scan says so.\"}");
        Response first = client().responses().create(ResponseCreateParams.builder().model(MODEL).instructions("You help recipients.")
                .input("Where is PX-AI-8120?").addTool(trackParcelTool()).build());
        first.validate();
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
        ResponseFunctionToolCall call = first.output().get(0).asFunctionCall();
        assertThat(call.name()).isEqualTo("track_parcel");

        // The follow-up names the first response and sends only the tool's result: the mock
        // reads it with the conversation it continues.
        Response second = client().responses().create(ResponseCreateParams.builder().model(MODEL).previousResponseId(first.id())
                .inputOfResponse(List.of(ResponseInputItem.ofFunctionCallOutput(ResponseInputItem.FunctionCallOutput.builder()
                        .callId(call.callId()).output("{\"status\":\"OUT_FOR_DELIVERY\"}").build())))
                .addTool(trackParcelTool()).build());
        second.validate();
        assertThat(Contracts.interaction(SPEC, last())).isEmpty();
        assertThat(second.output().get(0).isReasoning()).isTrue();
        assertThat(second.output().get(1).asMessage().content().get(0).asOutputText().text()).isEqualTo("PX-AI-8120 is out for delivery.");
    }

    @Test
    void aResponseStreamsTypedEvents() {
        answer("PX-AI-8121", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"reasoning\": \"It was scanned there.\", "
                + "\"toolCalls\": [{\"name\": \"notify_recipient\", \"arguments\": {\"reference\": \"PX-AI-8121\"}}]}");
        ResponseAccumulator acc = ResponseAccumulator.create();
        try (StreamResponse<ResponseStreamEvent> s = client().responses().createStreaming(ResponseCreateParams.builder().model(MODEL)
                .input("Where is PX-AI-8121?").build())) {
            s.stream().forEach(acc::accumulate);
        }
        Response r = acc.response();
        r.validate();
        assertThat(r.output()).hasSize(3);
        assertThat(r.output().get(1).asMessage().content().get(0).asOutputText().text()).isEqualTo("Your parcel is at the Leipzig depot.");
        assertThat(r.output().get(2).asFunctionCall().arguments()).isEqualTo("{\"reference\":\"PX-AI-8121\"}");
        for (String event : Contracts.sseData(last().getResponse().getBodyAsString())) {
            assertThat(Contracts.responseEvent(SPEC, event)).as(event).isEmpty();
        }
    }

    @Test
    void aResponseStreamFailsWithAnErrorEvent() {
        answer("PX-AI-8122", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"error\": {\"type\": \"rate_limit\", \"afterEvents\": 3}}");
        List<ResponseStreamEvent> events = new ArrayList<>();
        try (StreamResponse<ResponseStreamEvent> s = client().responses().createStreaming(ResponseCreateParams.builder().model(MODEL)
                .input("Where is PX-AI-8122?").build())) {
            s.stream().forEach(events::add);
        }
        assertThat(events).hasSize(4);
        assertThat(events.get(3).asError().code()).contains("rate_limit_exceeded");
        assertThat(Contracts.schema(SPEC, "ResponseErrorEvent", Contracts.sseData(last().getResponse().getBodyAsString()).get(3))).isEmpty();
    }
}
