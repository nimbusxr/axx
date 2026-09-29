// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.github.tomakehurst.wiremock.common.Json;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import software.amazon.awssdk.auth.credentials.AwsBasicCredentials;
import software.amazon.awssdk.auth.credentials.StaticCredentialsProvider;
import software.amazon.awssdk.awscore.exception.AwsServiceException;
import software.amazon.awssdk.awscore.retry.AwsRetryStrategy;
import software.amazon.awssdk.core.SdkBytes;
import software.amazon.awssdk.core.document.Document;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.services.bedrockruntime.BedrockRuntimeAsyncClient;
import software.amazon.awssdk.services.bedrockruntime.BedrockRuntimeClient;
import software.amazon.awssdk.services.bedrockruntime.model.ContentBlock;
import software.amazon.awssdk.services.bedrockruntime.model.ContentBlockDeltaEvent;
import software.amazon.awssdk.services.bedrockruntime.model.ConversationRole;
import software.amazon.awssdk.services.bedrockruntime.model.ConverseRequest;
import software.amazon.awssdk.services.bedrockruntime.model.ConverseResponse;
import software.amazon.awssdk.services.bedrockruntime.model.ConverseStreamRequest;
import software.amazon.awssdk.services.bedrockruntime.model.ConverseStreamResponseHandler;
import software.amazon.awssdk.services.bedrockruntime.model.InternalServerException;
import software.amazon.awssdk.services.bedrockruntime.model.InvokeModelRequest;
import software.amazon.awssdk.services.bedrockruntime.model.Message;
import software.amazon.awssdk.services.bedrockruntime.model.ServiceUnavailableException;
import software.amazon.awssdk.services.bedrockruntime.model.StopReason;
import software.amazon.awssdk.services.bedrockruntime.model.SystemContentBlock;
import software.amazon.awssdk.services.bedrockruntime.model.ThrottlingException;
import software.amazon.awssdk.services.bedrockruntime.model.Tool;
import software.amazon.awssdk.services.bedrockruntime.model.ToolConfiguration;
import software.amazon.awssdk.services.bedrockruntime.model.ToolInputSchema;
import software.amazon.awssdk.services.bedrockruntime.model.ToolResultBlock;
import software.amazon.awssdk.services.bedrockruntime.model.ToolResultContentBlock;
import software.amazon.awssdk.services.bedrockruntime.model.ToolSpecification;
import software.amazon.awssdk.services.bedrockruntime.model.ToolUseBlock;
import software.amazon.awssdk.services.bedrockruntime.model.ValidationException;

import java.net.URI;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;

/**
 * Amazon Bedrock's Converse API, and the embeddings of its Titan and Cohere models, as the official
 * AWS SDK reads them. The SDK is generated from Bedrock's Smithy model.
 */
class BedrockTest extends ModelMockTest {
    private static final String MODEL = "eu.amazon.nova-lite-v1:0";

    private final List<AutoCloseable> clients = new ArrayList<>();

    @AfterEach
    void closeClients() throws Exception {
        for (AutoCloseable c : clients) {
            c.close();
        }
    }

    private BedrockRuntimeClient client() {
        BedrockRuntimeClient c = BedrockRuntimeClient.builder().endpointOverride(URI.create(url())).region(Region.EU_CENTRAL_1)
                .credentialsProvider(StaticCredentialsProvider.create(AwsBasicCredentials.create("AKIDPARCELS", "parcels-secret")))
                .overrideConfiguration(o -> o.retryStrategy(AwsRetryStrategy.doNotRetry())).build();
        clients.add(c);
        return c;
    }

    private BedrockRuntimeAsyncClient async() {
        BedrockRuntimeAsyncClient c = BedrockRuntimeAsyncClient.builder().endpointOverride(URI.create(url())).region(Region.EU_CENTRAL_1)
                .credentialsProvider(StaticCredentialsProvider.create(AwsBasicCredentials.create("AKIDPARCELS", "parcels-secret")))
                .overrideConfiguration(o -> o.retryStrategy(AwsRetryStrategy.doNotRetry())).build();
        clients.add(c);
        return c;
    }

    private static Message user(String text) {
        return Message.builder().role(ConversationRole.USER).content(ContentBlock.fromText(text)).build();
    }

    private static ConverseRequest.Builder ask(String question) {
        return ConverseRequest.builder().modelId(MODEL).system(SystemContentBlock.fromText("You help recipients track their parcels."))
                .messages(user(question));
    }

    private static ToolConfiguration trackParcel() {
        return ToolConfiguration.builder().tools(Tool.fromToolSpec(ToolSpecification.builder().name("track_parcel")
                .description("Finds where a parcel is.")
                .inputSchema(ToolInputSchema.fromJson(Document.mapBuilder().putString("type", "object").build())).build())).build();
    }

    @Test
    void converseAnswersWithText() {
        answer("PX-AI-8401", "{\"text\": \"Your parcel PX-AI-8401 is out for delivery.\", \"reasoning\": \"The last scan says so.\"}");
        ConverseResponse r = client().converse(ask("Where is my parcel PX-AI-8401?").build());
        assertThat(last().getRequest().getUrl()).isEqualTo("/model/eu.amazon.nova-lite-v1%3A0/converse");
        List<ContentBlock> content = r.output().message().content();
        assertThat(content.get(0).reasoningContent().reasoningText().text()).isEqualTo("The last scan says so.");
        assertThat(content.get(1).text()).isEqualTo("Your parcel PX-AI-8401 is out for delivery.");
        assertThat(r.stopReason()).isEqualTo(StopReason.END_TURN);
        assertThat(r.usage().totalTokens()).isPositive();
        assertThat(r.metrics().latencyMs()).isNotNull();
    }

    @Test
    void aToolLoopIsWalkedTurnByTurn() {
        answer("PX-AI-8402", "{\"toolCalls\": [{\"name\": \"track_parcel\", \"arguments\": {\"reference\": \"PX-AI-8402\"}}]}");
        answerAfter("PX-AI-8402", "track_parcel", "{\"text\": \"PX-AI-8402 is out for delivery and arrives today.\"}");
        ConverseResponse first = client().converse(ask("Where is PX-AI-8402?").toolConfig(trackParcel()).build());
        assertThat(first.stopReason()).isEqualTo(StopReason.TOOL_USE);
        ToolUseBlock use = first.output().message().content().get(0).toolUse();
        assertThat(use.name()).isEqualTo("track_parcel");
        assertThat(use.input().asMap().get("reference").asString()).isEqualTo("PX-AI-8402");
        ConverseResponse second = client().converse(ask("Where is PX-AI-8402?").toolConfig(trackParcel())
                .messages(user("Where is PX-AI-8402?"), first.output().message(),
                        Message.builder().role(ConversationRole.USER).content(ContentBlock.fromToolResult(ToolResultBlock.builder()
                                .toolUseId(use.toolUseId())
                                .content(ToolResultContentBlock.fromJson(Document.mapBuilder().putString("status", "OUT_FOR_DELIVERY").build()))
                                .build())).build())
                .build());
        assertThat(second.output().message().content().get(0).text()).isEqualTo("PX-AI-8402 is out for delivery and arrives today.");
    }

    @Test
    void earlyStops() {
        answer("PX-AI-8403", "{\"text\": \"Your parcel is\", \"stop\": \"length\"}");
        answer("PX-AI-8404", "{\"stop\": \"safety\"}");
        assertThat(client().converse(ask("PX-AI-8403?").build()).stopReason()).isEqualTo(StopReason.MAX_TOKENS);
        assertThat(client().converse(ask("PX-AI-8404?").build()).stopReason()).isEqualTo(StopReason.CONTENT_FILTERED);
    }

    /** What a ConverseStream sent, as the SDK's handler saw it. */
    private static final class Heard {
        final StringBuilder text = new StringBuilder();
        final StringBuilder reasoning = new StringBuilder();
        final StringBuilder input = new StringBuilder();
        final List<String> tools = new ArrayList<>();
        final List<String> events = new ArrayList<>();
        StopReason stop;
        Long outputTokens;

        ConverseStreamResponseHandler handler() {
            return ConverseStreamResponseHandler.builder().subscriber(ConverseStreamResponseHandler.Visitor.builder()
                    .onMessageStart(e -> events.add("messageStart"))
                    .onContentBlockStart(e -> {
                        events.add("contentBlockStart");
                        tools.add(e.start().toolUse().name());
                    })
                    .onContentBlockDelta(this::delta)
                    .onContentBlockStop(e -> events.add("contentBlockStop"))
                    .onMessageStop(e -> {
                        events.add("messageStop");
                        stop = e.stopReason();
                    })
                    .onMetadata(e -> {
                        events.add("metadata");
                        outputTokens = e.usage().outputTokens().longValue();
                    })
                    .build()).build();
        }

        private void delta(ContentBlockDeltaEvent e) {
            events.add("contentBlockDelta");
            if (e.delta().text() != null) {
                text.append(e.delta().text());
            } else if (e.delta().toolUse() != null) {
                input.append(e.delta().toolUse().input());
            } else if (e.delta().reasoningContent() != null && e.delta().reasoningContent().text() != null) {
                reasoning.append(e.delta().reasoningContent().text());
            }
        }
    }

    private static ConverseStreamRequest stream(String question) {
        return ConverseStreamRequest.builder().modelId(MODEL).messages(user(question)).build();
    }

    @Test
    void converseStreamIsAnAwsEventStream() throws Exception {
        answer("PX-AI-8405", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"reasoning\": \"It was scanned there.\", "
                + "\"toolCalls\": [{\"name\": \"notify_recipient\", \"arguments\": {\"reference\": \"PX-AI-8405\"}}]}");
        Heard heard = new Heard();
        async().converseStream(stream("Where is PX-AI-8405?"), heard.handler()).get(10, TimeUnit.SECONDS);
        assertThat(heard.reasoning).hasToString("It was scanned there.");
        assertThat(heard.text).hasToString("Your parcel is at the Leipzig depot.");
        assertThat(heard.tools).containsExactly("notify_recipient");
        assertThat(heard.input).hasToString("{\"reference\":\"PX-AI-8405\"}");
        assertThat(heard.stop).isEqualTo(StopReason.TOOL_USE);
        assertThat(heard.outputTokens).isPositive();
        assertThat(heard.events.get(0)).isEqualTo("messageStart");
        assertThat(heard.events.get(heard.events.size() - 1)).isEqualTo("metadata");
    }

    @Test
    void converseStreamFailsWithAnExceptionEvent() {
        answer("PX-AI-8406", "{\"text\": \"Your parcel is at the Leipzig depot.\", \"error\": {\"type\": \"rate_limit\", \"afterEvents\": 3}}");
        Heard heard = new Heard();
        assertThatThrownBy(() -> async().converseStream(stream("Where is PX-AI-8406?"), heard.handler()).get(10, TimeUnit.SECONDS))
                .isInstanceOf(ExecutionException.class).cause().isInstanceOf(ThrottlingException.class)
                .hasMessageContaining("Too many requests");
        assertThat(heard.events).hasSize(3);
    }

    @Test
    void errorsAreBedrocks() {
        answer("PX-AI-8410", "{\"error\": {\"type\": \"rate_limit\"}}");
        answer("PX-AI-8411", "{\"error\": {\"type\": \"overloaded\"}}");
        answer("PX-AI-8412", "{\"error\": {\"type\": \"context_length\"}}");
        answer("PX-AI-8413", "{\"error\": {\"type\": \"auth\"}}");
        answer("PX-AI-8414", "{\"error\": {\"type\": \"server\"}}");
        assertThatThrownBy(() -> client().converse(ask("PX-AI-8410").build())).isInstanceOf(ThrottlingException.class);
        assertThatThrownBy(() -> client().converse(ask("PX-AI-8411").build())).isInstanceOf(ServiceUnavailableException.class);
        assertThatThrownBy(() -> client().converse(ask("PX-AI-8412").build())).isInstanceOf(ValidationException.class)
                .hasMessageContaining("Input is too long");
        assertThatThrownBy(() -> client().converse(ask("PX-AI-8413").build()))
                .isInstanceOfSatisfying(AwsServiceException.class, e -> {
                    assertThat(e.statusCode()).isEqualTo(403);
                    assertThat(e.awsErrorDetails().errorCode()).isEqualTo("UnrecognizedClientException");
                });
        assertThatThrownBy(() -> client().converse(ask("PX-AI-8414").build())).isInstanceOf(InternalServerException.class);
    }

    @Test
    @SuppressWarnings("unchecked")
    void titanAndCohereEmbeddings() {
        stub(mapping("{\"endpoint\": \"embeddings\"}", "{}"));
        String titan = client().invokeModel(InvokeModelRequest.builder().modelId("amazon.titan-embed-text-v2:0").contentType("application/json")
                .body(SdkBytes.fromUtf8String("{\"inputText\": \"Lindenweg 14, 04109 Leipzig\", \"dimensions\": 256}")).build()).body().asUtf8String();
        List<Number> vector = (List<Number>) Json.read(titan, Map.class).get("embedding");
        assertThat(vector.stream().map(Number::floatValue).toList()).isEqualTo(Wire.vector("Lindenweg 14, 04109 Leipzig", 256));
        String cohere = client().invokeModel(InvokeModelRequest.builder().modelId("cohere.embed-multilingual-v3").contentType("application/json")
                .body(SdkBytes.fromUtf8String("{\"texts\": [\"Lindenweg 14\", \"Hauptstraße 5\"], \"input_type\": \"search_document\"}"))
                .build()).body().asUtf8String();
        Map<String, Object> c = Json.read(cohere, Map.class);
        assertThat((List<List<Number>>) c.get("embeddings")).hasSize(2).allSatisfy(v -> assertThat(v).hasSize(1024));
        assertThat(c.get("response_type")).isEqualTo("embeddings_floats");
    }
}
