// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import com.github.tomakehurst.wiremock.extension.ResponseTransformerV2;
import com.github.tomakehurst.wiremock.http.HttpHeader;
import com.github.tomakehurst.wiremock.http.HttpHeaders;
import com.github.tomakehurst.wiremock.http.Request;
import com.github.tomakehurst.wiremock.http.Response;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * The {@code model-answer} transformer: renders the stub's answer, written the same way for every
 * provider, in the format of the request it answers: OpenAI's Chat Completions and Responses,
 * Anthropic's Messages (directly, on Bedrock and on Vertex AI), Gemini, Bedrock's Converse, and
 * Ollama, streamed when the request asks for a stream. The stub's other headers are kept, and its
 * delays apply.
 */
public class ModelAnswer implements ResponseTransformerV2 {
    static final String NAME = "model-answer";

    private static final Logger log = LoggerFactory.getLogger(ModelAnswer.class);
    private static final Set<String> OURS = Set.of("content-type", "content-length", "transfer-encoding");

    private final Memory memory;

    ModelAnswer(Memory memory) {
        this.memory = memory;
    }

    @Override
    public String getName() {
        return NAME;
    }

    @Override
    public boolean applyGlobally() {
        return false;
    }

    @Override
    public Response transform(Response response, ServeEvent serveEvent) {
        Request request = serveEvent.getRequest();
        ModelCall call = ModelCall.read(request, memory);
        if (call == null) {
            return failure(response, "the model-answer transformer answers requests to models (chat completions, responses, messages, "
                    + "generateContent, converse, /api/chat...), and their embeddings and models, not " + request.getMethod() + " " + request.getUrl());
        }
        Rendered r;
        try {
            r = Wire.of(call.kind(), memory).render(call, Answer.read(response.getBodyAsString()));
        } catch (IllegalArgumentException | IllegalStateException e) {
            return failure(response, e.getMessage());
        }
        List<HttpHeader> headers = new ArrayList<>();
        for (Map.Entry<String, String> h : r.headers().entrySet()) {
            headers.add(new HttpHeader(h.getKey(), h.getValue()));
        }
        for (HttpHeader h : response.getHeaders().all()) {
            if (!OURS.contains(h.key().toLowerCase()) && r.headers().keySet().stream().noneMatch(h.key()::equalsIgnoreCase)) {
                headers.add(h);
            }
        }
        return Response.Builder.like(response).but().status(r.status()).headers(new HttpHeaders(headers)).body(r.body()).build();
    }

    private static Response failure(Response response, String message) {
        log.error("the axx model mock cannot answer: {}", message);
        return Response.Builder.like(response)
                .but()
                .status(500)
                .headers(new HttpHeaders(new HttpHeader("Content-Type", "text/plain; charset=utf-8")))
                .body(("the axx model mock cannot answer: " + message).getBytes(StandardCharsets.UTF_8))
                .build();
    }
}
