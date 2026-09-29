// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import com.github.tomakehurst.wiremock.extension.Parameters;
import com.github.tomakehurst.wiremock.http.Request;
import com.github.tomakehurst.wiremock.matching.MatchResult;
import com.github.tomakehurst.wiremock.matching.RequestMatcherExtension;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * The {@code model-request} matcher: a request to a model, of any provider's API, about the
 * stub's texts. Its parameters:
 *
 * <ul>
 *   <li>{@code about}: a text, or texts, the conversation has, in any message, the system prompt
 *       and the tools' results included (for embeddings, the texts to embed).
 *   <li>{@code afterTool}: the tool whose result the model was given last, since the user last
 *       wrote. A stub without it answers only when the model has had no tool result since.
 *   <li>{@code endpoint}: {@code chat} (the default), {@code embeddings} or {@code models}.
 * </ul>
 */
public class ModelRequest extends RequestMatcherExtension {
    static final String NAME = "model-request";

    private static final Logger log = LoggerFactory.getLogger(ModelRequest.class);

    private final Memory memory;

    ModelRequest(Memory memory) {
        this.memory = memory;
    }

    @Override
    public String getName() {
        return NAME;
    }

    @Override
    public MatchResult match(Request request, Parameters parameters) {
        Criteria criteria;
        try {
            criteria = Criteria.of(parameters);
        } catch (IllegalArgumentException e) {
            log.error("a stub's model-request matcher matches nothing: {}", e.getMessage());
            return MatchResult.noMatch();
        }
        ModelCall call = ModelCall.read(request, memory);
        return MatchResult.of(call != null && criteria.matches(call));
    }
}
