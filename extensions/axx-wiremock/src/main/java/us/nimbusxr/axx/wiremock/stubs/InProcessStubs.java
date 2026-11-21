// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.stubs;

import com.github.tomakehurst.wiremock.common.FileSource;
import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.common.Metadata;
import com.github.tomakehurst.wiremock.http.ImmutableRequest;
import com.github.tomakehurst.wiremock.http.Request;
import com.github.tomakehurst.wiremock.http.RequestMethod;
import com.github.tomakehurst.wiremock.http.ResponseDefinition;
import com.github.tomakehurst.wiremock.matching.RequestPattern;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;
import com.github.tomakehurst.wiremock.stubbing.StubMappings;

import java.net.URI;
import java.net.URISyntaxException;
import java.nio.charset.StandardCharsets;
import java.util.Optional;

/**
 * The stubs a mock asks in-process: an ordinary WireMock stub of a POST to a path the mock makes
 * up (a tool of an MCP server, the messages of an A2A agent), whose request body is what the mock
 * looks up. The stubs are matched with WireMock's own matchers, in WireMock's order (priority,
 * then the newest first), against a request that is never sent: the lookups are not requests,
 * and the journal records only the requests the mock received.
 */
public final class InProcessStubs {
    private InProcessStubs() {}

    /**
     * The first stub, in WireMock's order, whose request matches a POST of a JSON body to a path.
     * Stubs whose metadata has the mock's endpoint key (the mock's own endpoints) are skipped.
     */
    public static Optional<StubMapping> find(StubMappings stubs, String path, String jsonBody, String endpointKey) {
        if (stubs == null) {
            return Optional.empty();
        }
        Request probe =
                new ImmutableRequest.Builder()
                        .withAbsoluteUrl("http://localhost" + path)
                        .withMethod(RequestMethod.POST)
                        .withHeader("Content-Type", "application/json")
                        .withBody(jsonBody.getBytes(StandardCharsets.UTF_8))
                        .build();
        for (StubMapping m : stubs.getAll()) {
            Metadata md = m.getMetadata();
            if (md != null && md.containsKey(endpointKey)) {
                continue;
            }
            if (m.getRequest().match(probe).isExactMatch()) {
                return Optional.of(m);
            }
        }
        return Optional.empty();
    }

    /**
     * A stub's body as text: its {@code jsonBody}, its {@code body}, or its {@code bodyFileName}'s
     * file under WireMock's {@code __files}; null when it has none.
     */
    public static String body(ResponseDefinition stub, FileSource root) {
        if (stub.getJsonBody() != null) {
            return stub.getJsonBody().toString();
        }
        if (stub.getBody() != null) {
            return stub.getBody();
        }
        if (stub.getBodyFileName() != null && root != null) {
            return new String(root.child("__files").getBinaryFileNamed(stub.getBodyFileName()).readContents(), StandardCharsets.UTF_8);
        }
        return null;
    }

    /** A stub's body read now, for checking it when it is added: only its jsonBody or body. */
    public static String inlineBody(ResponseDefinition stub) {
        if (stub.getJsonBody() != null) {
            return stub.getJsonBody().toString();
        }
        return stub.getBody();
    }

    /** The exact path a stub's request matches ({@code urlPath}, or {@code url} without its query), or null. */
    public static String exactPath(RequestPattern request) {
        if (request.getUrlPath() != null) {
            return request.getUrlPath();
        }
        String url = request.getUrl();
        if (url != null) {
            int q = url.indexOf('?');
            return q < 0 ? url : url.substring(0, q);
        }
        return null;
    }

    /** A stub's name for a message: its name, or its request when it has none (a mapping file's). */
    public static String describe(StubMapping stub) {
        return stub.getName() != null ? stub.getName() : Json.write(stub.getRequest()).replaceAll("\\s+", " ");
    }

    /** A path segment, percent-encoded where it has characters a URL path cannot hold. */
    public static String segment(String s) {
        try {
            return new URI(null, null, "/" + s, null).getRawPath().substring(1).replace("/", "%2F").replace("?", "%3F");
        } catch (URISyntaxException e) {
            throw new IllegalArgumentException("cannot use " + s + " in a URL path", e);
        }
    }
}
