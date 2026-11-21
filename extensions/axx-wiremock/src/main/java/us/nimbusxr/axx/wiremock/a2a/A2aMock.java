// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import static com.github.tomakehurst.wiremock.client.WireMock.urlPathEqualTo;
import static com.github.tomakehurst.wiremock.client.WireMock.urlPathMatching;
import static com.github.tomakehurst.wiremock.matching.RequestPatternBuilder.newRequestPattern;

import com.github.tomakehurst.wiremock.client.ResponseDefinitionBuilder;
import com.github.tomakehurst.wiremock.common.FileSource;
import com.github.tomakehurst.wiremock.common.Metadata;
import com.github.tomakehurst.wiremock.extension.MappingsLoaderExtension;
import com.github.tomakehurst.wiremock.extension.Parameters;
import com.github.tomakehurst.wiremock.extension.ResponseTransformerV2;
import com.github.tomakehurst.wiremock.http.HttpHeader;
import com.github.tomakehurst.wiremock.http.HttpHeaders;
import com.github.tomakehurst.wiremock.http.Request;
import com.github.tomakehurst.wiremock.http.RequestMethod;
import com.github.tomakehurst.wiremock.http.Response;
import com.github.tomakehurst.wiremock.http.ResponseDefinition;
import com.github.tomakehurst.wiremock.matching.UrlPattern;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;
import com.github.tomakehurst.wiremock.stubbing.StubMappings;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.net.URI;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * An A2A agent (A2A 1.0), mocked from its card and answered from stubs.
 *
 * <p>The card ({@code A2A_AGENT_CARD_SOURCE}) is served at {@code /.well-known/agent-card.json},
 * and the mock answers the interfaces it lists at their URLs' paths: the JSON-RPC binding (one
 * POST endpoint; streams as server-sent events of JSON-RPC responses) and the HTTP+JSON binding
 * ({@code message:send}, {@code message:stream}, {@code tasks}...; errors as a {@code
 * google.rpc.Status} with its A2A {@code ErrorInfo}). A gRPC interface is not mocked.
 *
 * <p>What the agent answers comes from stubs (see {@link Agent}): the journal records the
 * requests the mock received, and the stub lookups are not requests. A request that names an
 * A2A version ({@code A2A-Version}) the interface does not speak is refused; one that names none
 * is answered.
 */
public class A2aMock implements MappingsLoaderExtension, ResponseTransformerV2 {
    static final String NAME = "a2a";
    static final String CARD_PATH = "/.well-known/agent-card.json";
    static final String REST_TYPE = "application/a2a+json";

    private static final Logger log = LoggerFactory.getLogger(A2aMock.class);
    private static final Pattern TASK = Pattern.compile("/tasks/([^/:]+)(:cancel|:subscribe|/pushNotificationConfigs(/[^/]+)?)?");
    private static final Set<String> PUSH_METHODS = Set.of("CreateTaskPushNotificationConfig", "GetTaskPushNotificationConfig",
            "ListTaskPushNotificationConfigs", "DeleteTaskPushNotificationConfig");

    private final AgentCard card;
    private final Tasks tasks;
    private final Agent agent;
    private volatile StubMappings stubs;

    A2aMock(AgentCard card, Tasks tasks, FileSource files) {
        this.card = card;
        this.tasks = tasks;
        this.agent = new Agent(card, tasks, () -> stubs, files);
        for (AgentCard.Interface i : card.interfaces) {
            if (!i.binding().equals(AgentCard.Interface.JSONRPC) && !i.binding().equals(AgentCard.Interface.REST)) {
                log.warn("the A2A mock of {} answers the JSONRPC and HTTP+JSON bindings: it does not answer the {} interface {}", card.name, i.binding(), i.url());
            } else if (!i.version().startsWith("1.")) {
                log.warn("the A2A mock of {} answers in A2A 1.0: the interface {} says protocolVersion {}", card.name, i.url(), i.version());
            }
        }
    }

    @Override
    public String getName() {
        return NAME;
    }

    @Override
    public boolean applyGlobally() {
        return false;
    }

    /**
     * Adds the card's stub and the interfaces' endpoints, at startup and whenever WireMock resets
     * to its mappings; a reset forgets the tasks, too.
     */
    @Override
    public void loadMappingsInto(StubMappings stubMappings) {
        this.stubs = stubMappings;
        tasks.clear();
        stubMappings.addMapping(endpoint("card", RequestMethod.GET, urlPathEqualTo(CARD_PATH),
                ResponseDefinitionBuilder.responseDefinition().withStatus(200).withHeader("Content-Type", "application/json").withBody(card.text).build()));
        Set<String> seen = new HashSet<>();
        for (AgentCard.Interface i : card.interfaces) {
            if (!seen.add(i.binding() + " " + i.path())) {
                continue;
            }
            Parameters p = Parameters.from(Map.of("binding", i.binding(), "base", i.path(), "version", i.version()));
            ResponseDefinition answer = ResponseDefinitionBuilder.responseDefinition().withStatus(200).withTransformer(NAME, "interface", p).build();
            if (i.binding().equals(AgentCard.Interface.JSONRPC)) {
                stubMappings.addMapping(endpoint(i.url(), RequestMethod.POST, urlPathEqualTo(i.path().isEmpty() ? "/" : i.path()), answer));
            } else if (i.binding().equals(AgentCard.Interface.REST)) {
                String routes = Pattern.quote(i.path())
                        + "/(message:send|message:stream|tasks|extendedAgentCard|tasks/[^/]+(/pushNotificationConfigs(/[^/]+)?)?)";
                stubMappings.addMapping(endpoint(i.url(), RequestMethod.ANY, urlPathMatching(routes), answer));
            }
        }
    }

    private StubMapping endpoint(String what, RequestMethod method, UrlPattern url, ResponseDefinition response) {
        StubMapping s = new StubMapping(newRequestPattern(method, url).build(), response);
        s.setId(UUID.nameUUIDFromBytes(("axx-a2a:" + method + ":" + what + ":" + url.getPattern().getExpected()).getBytes(StandardCharsets.UTF_8)));
        s.setName("A2A agent " + card.name + ": " + what);
        s.setPriority(10);
        s.setMetadata(Metadata.metadata().attr(Agent.ENDPOINT_KEY, true).build());
        return s;
    }

    @Override
    public Response transform(Response response, ServeEvent serveEvent) {
        Map<String, Object> p = Values.map(serveEvent.getTransformerParameters().get("interface"));
        String binding = p == null ? AgentCard.Interface.JSONRPC : String.valueOf(p.get("binding"));
        String base = p == null ? "" : String.valueOf(p.get("base"));
        String version = p == null ? "1.0" : String.valueOf(p.get("version"));
        Request request = serveEvent.getRequest();
        Answer a;
        try {
            a = binding.equals(AgentCard.Interface.REST) ? rest(request, base, version) : jsonRpc(request, version);
        } catch (RuntimeException e) {
            log.error("the A2A mock of {} failed", card.name, e);
            a = new Answer(500, "application/json", Values.write(Values.obj("error", Values.obj("code", 500, "status", "INTERNAL",
                    "message", "the A2A mock of " + card.name + " failed: " + e.getMessage()))));
        }
        return Response.Builder.like(response)
                .but()
                .status(a.status())
                .headers(new HttpHeaders(new HttpHeader("Content-Type", a.contentType())))
                .body(a.body().getBytes(StandardCharsets.UTF_8))
                .build();
    }

    /** An answer: its status, content type and body. */
    record Answer(int status, String contentType, String body) {}

    private Answer jsonRpc(Request request, String version) {
        Object parsed;
        try {
            parsed = Values.parse(request.getBodyAsString());
        } catch (IllegalArgumentException e) {
            return jsonRpcError(null, new A2aError(A2aError.PARSE_ERROR, "Invalid JSON payload: " + e.getMessage()));
        }
        Map<String, Object> call = Values.map(parsed);
        Object id = call == null ? null : call.get("id");
        String method = call == null ? null : Values.text(call.get("method"));
        if (method == null) {
            return jsonRpcError(id, new A2aError(A2aError.INVALID_REQUEST, "Request payload validation error: a JSON-RPC request is an object with a method"));
        }
        Object params = call.get("params");
        Map<String, Object> request2 = params == null ? new LinkedHashMap<>() : Values.map(params);
        try {
            checkVersion(request, version);
            if (request2 == null) {
                throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: the params of " + method + " are an object");
            }
            return switch (method) {
                case "SendMessage" -> jsonRpcResult(id, agent.send(request2, false).result());
                case "SendStreamingMessage" -> jsonRpcStream(id, agent.send(request2, true).events());
                case "GetTask" -> jsonRpcResult(id, agent.get(request2));
                case "ListTasks" -> jsonRpcResult(id, agent.list(request2));
                case "CancelTask" -> jsonRpcResult(id, agent.cancel(request2));
                case "SubscribeToTask" -> jsonRpcStream(id, agent.subscribe(request2).events());
                case "GetExtendedAgentCard" -> throw extendedCard();
                default -> {
                    if (PUSH_METHODS.contains(method)) {
                        throw pushNotifications();
                    }
                    throw new A2aError(A2aError.METHOD_NOT_FOUND, "Method not found: " + method);
                }
            };
        } catch (A2aError e) {
            return jsonRpcError(id, e);
        }
    }

    private Answer rest(Request request, String base, String version) {
        String path = URI.create(request.getUrl()).getRawPath().substring(base.length());
        RequestMethod method = request.getMethod();
        try {
            checkVersion(request, version);
            if (path.equals("/message:send") || path.equals("/message:stream")) {
                allow(method, RequestMethod.POST);
                Map<String, Object> body = restBody(request);
                boolean stream = path.equals("/message:stream");
                Agent.Outcome o = agent.send(body, stream);
                return stream ? restStream(o.events()) : restResult(o.result());
            }
            if (path.equals("/tasks")) {
                allow(method, RequestMethod.GET);
                return restResult(agent.list(query(request)));
            }
            if (path.equals("/extendedAgentCard")) {
                allow(method, RequestMethod.GET);
                throw extendedCard();
            }
            Matcher m = TASK.matcher(path);
            if (m.matches()) {
                Map<String, Object> q = query(request);
                q.put("id", URLDecoder.decode(m.group(1), StandardCharsets.UTF_8));
                String action = m.group(2);
                if (action == null) {
                    allow(method, RequestMethod.GET);
                    return restResult(agent.get(q));
                }
                if (action.equals(":cancel")) {
                    allow(method, RequestMethod.POST);
                    return restResult(agent.cancel(q));
                }
                if (action.equals(":subscribe")) {
                    allow(method, RequestMethod.GET, RequestMethod.POST);
                    return restStream(agent.subscribe(q).events());
                }
                throw pushNotifications();
            }
            throw new A2aError(A2aError.METHOD_NOT_FOUND, "Method not found: " + method + " " + path + " is not an operation of A2A's HTTP+JSON binding");
        } catch (A2aError e) {
            return new Answer(e.rest().status(), REST_TYPE, Values.write(e.restBody()));
        } catch (MethodNotAllowed e) {
            A2aError err = new A2aError(A2aError.METHOD_NOT_FOUND, e.getMessage());
            return new Answer(405, REST_TYPE, Values.write(err.restBody()));
        }
    }

    private static void allow(RequestMethod method, RequestMethod... allowed) {
        for (RequestMethod a : allowed) {
            if (a.equals(method)) {
                return;
            }
        }
        throw new MethodNotAllowed("Method not allowed: this operation of A2A's HTTP+JSON binding is " + List.of(allowed) + ", not " + method);
    }

    private static Map<String, Object> restBody(Request request) {
        try {
            Map<String, Object> body = Values.map(Values.parse(request.getBodyAsString()));
            if (body == null) {
                throw new A2aError(A2aError.INVALID_REQUEST, "Request payload validation error: the body is a JSON object, a SendMessageRequest");
            }
            return body;
        } catch (IllegalArgumentException e) {
            throw new A2aError(A2aError.PARSE_ERROR, "Invalid JSON payload: " + e.getMessage());
        }
    }

    /** The query parameters, as a request's fields: the last value of each. */
    private static Map<String, Object> query(Request request) {
        Map<String, Object> q = new LinkedHashMap<>();
        String raw = URI.create(request.getUrl()).getRawQuery();
        if (raw == null) {
            return q;
        }
        for (String pair : raw.split("&")) {
            int eq = pair.indexOf('=');
            String k = URLDecoder.decode(eq < 0 ? pair : pair.substring(0, eq), StandardCharsets.UTF_8);
            q.put(k, eq < 0 ? "" : URLDecoder.decode(pair.substring(eq + 1), StandardCharsets.UTF_8));
        }
        return q;
    }

    /** Refuses a request that names an A2A version the interface does not speak (a request that names none is answered). */
    private static void checkVersion(Request request, String version) {
        String asked = request.getHeader("A2A-Version");
        if (asked == null) {
            String raw = URI.create(request.getUrl()).getRawQuery();
            if (raw != null) {
                for (String pair : raw.split("&")) {
                    if (pair.startsWith("A2A-Version=")) {
                        asked = URLDecoder.decode(pair.substring("A2A-Version=".length()), StandardCharsets.UTF_8);
                    }
                }
            }
        }
        if (asked != null && !asked.isBlank() && !majorMinor(asked).equals(majorMinor(version))) {
            throw new A2aError(A2aError.VERSION_NOT_SUPPORTED, "A2A version " + asked + " is not supported: this interface speaks " + version);
        }
    }

    private static String majorMinor(String v) {
        String[] parts = v.strip().split("\\.");
        return parts[0] + "." + (parts.length > 1 ? parts[1] : "0");
    }

    private A2aError extendedCard() {
        return card.extendedAgentCard
                ? new A2aError(A2aError.EXTENDED_AGENT_CARD_NOT_CONFIGURED, "the agent has no extended agent card: the mock serves its public card only")
                : new A2aError(A2aError.UNSUPPORTED_OPERATION, "the agent has no extended agent card: its card's capabilities.extendedAgentCard is not true");
    }

    private static A2aError pushNotifications() {
        return new A2aError(A2aError.PUSH_NOTIFICATION_NOT_SUPPORTED, "Push notifications are not supported: the A2A mock does not send them");
    }

    private static Answer jsonRpcResult(Object id, Object result) {
        return new Answer(200, "application/json", Values.write(Values.obj("jsonrpc", "2.0", "id", id, "result", result)));
    }

    private static Answer jsonRpcError(Object id, A2aError e) {
        return new Answer(200, "application/json", Values.write(Values.obj("jsonrpc", "2.0", "id", id, "error", e.jsonRpc())));
    }

    private static Answer jsonRpcStream(Object id, List<Object> events) {
        StringBuilder b = new StringBuilder();
        for (Object e : events) {
            b.append("data: ").append(Values.write(Values.obj("jsonrpc", "2.0", "id", id, "result", e))).append("\n\n");
        }
        return new Answer(200, "text/event-stream", b.toString());
    }

    private static Answer restResult(Object result) {
        return new Answer(200, REST_TYPE, Values.write(result));
    }

    private static Answer restStream(List<Object> events) {
        StringBuilder b = new StringBuilder();
        for (Object e : events) {
            b.append("data: ").append(Values.write(e)).append("\n\n");
        }
        return new Answer(200, "text/event-stream", b.toString());
    }

    /** A request with an HTTP method the operation does not take. */
    private static final class MethodNotAllowed extends RuntimeException {
        private static final long serialVersionUID = 1L;

        MethodNotAllowed(String message) {
            super(message);
        }
    }
}
