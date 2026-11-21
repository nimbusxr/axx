// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import com.atlassian.oai.validator.OpenApiInteractionValidator;
import com.atlassian.oai.validator.model.Request;
import com.atlassian.oai.validator.model.Response;
import com.atlassian.oai.validator.model.SimpleRequest;
import com.atlassian.oai.validator.model.SimpleResponse;
import com.atlassian.oai.validator.report.LevelResolver;
import com.atlassian.oai.validator.report.ValidationReport;
import com.github.tomakehurst.wiremock.common.Urls;
import com.github.tomakehurst.wiremock.http.HttpHeader;
import com.github.tomakehurst.wiremock.http.QueryParameter;
import com.github.tomakehurst.wiremock.verification.LoggedRequest;

import java.net.URI;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;

/** Validates a WireMock request/response pair against an OpenAPI specification. */
final class OpenApiValidator {
    private OpenApiValidator() {}

    /**
     * Loads a spec. Every finding is reported at ERROR here; the configured levels are applied by
     * the caller, so a finding a stub or axx relaxes is still recorded.
     */
    static OpenApiInteractionValidator load(String source, Optional<String[]> authHeader) {
        OpenApiInteractionValidator.Builder b =
                OpenApiInteractionValidator.createForSpecificationUrl(source)
                        .withLevelResolver(
                                LevelResolver.create()
                                        .withDefaultLevel(ValidationReport.Level.ERROR)
                                        .build());
        authHeader.ifPresent(h -> b.withAuthHeaderData(h[0], h[1]));
        return b.build();
    }

    /** The findings for a served request and the stubbed response, all at ERROR. */
    static List<Finding> findings(
            OpenApiInteractionValidator validator,
            LoggedRequest loggedRequest,
            com.github.tomakehurst.wiremock.http.Response stubbedResponse) {
        ValidationReport report =
                validator.validate(convertRequest(loggedRequest), convertResponse(stubbedResponse));
        List<Finding> out = new ArrayList<>();
        for (ValidationReport.Message m : report.getMessages()) {
            List<String> details = new ArrayList<>(m.getAdditionalInfo());
            for (ValidationReport.Message nested : m.getNestedMessages()) {
                details.add(nested.getKey() + ": " + nested.getMessage());
            }
            out.add(new Finding(m.getKey(), Level.ERROR, side(m), m.getMessage(), details));
        }
        return out;
    }

    private static String side(ValidationReport.Message m) {
        Optional<ValidationReport.MessageContext.Location> location =
                m.getContext().flatMap(ValidationReport.MessageContext::getLocation);
        if (location.isPresent()) {
            return location.get() == ValidationReport.MessageContext.Location.RESPONSE
                    ? "response"
                    : "request";
        }
        return m.getKey().startsWith("validation.response") ? "response" : "request";
    }

    private static Request convertRequest(final LoggedRequest request) {
        // The path without the query string: the query is passed as parameters, and a path
        // with it matches none of the spec's paths (unless a template variable swallows it).
        URI uri = URI.create(request.getUrl());
        final SimpleRequest.Builder builder =
                new SimpleRequest.Builder(request.getMethod().toString(), uri.getRawPath());

        final Map<String, QueryParameter> queryParameters = Urls.splitQuery(uri);
        queryParameters.forEach((k, v) -> builder.withQueryParam(v.key(), v.values()));

        // Every value of repeated headers, not only the first.
        for (HttpHeader header : request.getHeaders().all()) {
            builder.withHeader(header.key(), header.values());
        }

        return builder.withBody(request.getBody()).build();
    }

    private static Response convertResponse(
            final com.github.tomakehurst.wiremock.http.Response response) {
        final SimpleResponse.Builder builder =
                new SimpleResponse.Builder(response.getStatus()).withBody(response.getBody());
        response.getHeaders()
                .all()
                .forEach(header -> builder.withHeader(header.key(), header.values()));

        return builder.build();
    }
}
