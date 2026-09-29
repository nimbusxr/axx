// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import static com.github.tomakehurst.wiremock.client.WireMock.urlPathEqualTo;
import static com.github.tomakehurst.wiremock.matching.RequestPatternBuilder.newRequestPattern;

import com.github.tomakehurst.wiremock.client.ResponseDefinitionBuilder;
import com.github.tomakehurst.wiremock.common.FileSource;
import com.github.tomakehurst.wiremock.common.Metadata;
import com.github.tomakehurst.wiremock.extension.MappingsLoaderExtension;
import com.github.tomakehurst.wiremock.extension.ResponseTransformerV2;
import com.github.tomakehurst.wiremock.http.HttpHeader;
import com.github.tomakehurst.wiremock.http.HttpHeaders;
import com.github.tomakehurst.wiremock.http.Request;
import com.github.tomakehurst.wiremock.http.RequestMethod;
import com.github.tomakehurst.wiremock.http.Response;
import com.github.tomakehurst.wiremock.http.ResponseDefinition;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;
import com.github.tomakehurst.wiremock.stubbing.StubMappings;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import us.nimbusxr.axx.wiremock.stubs.InProcessStubs;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.UUID;

/**
 * An MCP server, mocked from its description and answered from stubs, over MCP's Streamable HTTP
 * transport: one POST per JSON-RPC message to the MCP endpoint ({@code MCP_PATH}), answered with
 * a single JSON object. It speaks the protocol version 2026-07-28, where every request carries its
 * version in {@code params._meta} and there are no sessions (clients may call {@code
 * server/discover} first), and the earlier versions 2025-11-25, 2025-06-18 and 2025-03-26, which
 * start with {@code initialize}.
 *
 * <p>The description ({@code MCP_SERVER_SOURCE}) is what the server lists: its tools, resources,
 * resource templates and prompts. Stubs are what it answers, looked up in-process as POSTs to
 * paths under the endpoint:
 *
 * <ul>
 *   <li>a tool call, {@code <path>/tools/<tool>} with the call's arguments as the body, once the
 *       arguments match the tool's input schema (arguments that do not are a tool error, {@code
 *       isError: true}, listing the problems); the stub's JSON body is the tool's result ({@code
 *       structuredContent}, {@code content}, {@code isError}), and its structured content must
 *       match the tool's output schema;
 *   <li>a resource read, {@code <path>/resources} with {@code {"uri": ...}} as the body; the stub
 *       answers {@code {"contents": [...]}}, and without one the description's inline content
 *       does;
 *   <li>a prompt, {@code <path>/prompts/<prompt>} with the prompt's arguments as the body; the
 *       stub answers {@code {"description", "messages"}}.
 * </ul>
 *
 * <p>A stub's {@code {"error": {"code", "message", "data"}}} answers a JSON-RPC error instead. A
 * call no stub answers is a JSON-RPC internal error that says so. The journal records the
 * JSON-RPC messages the mock received; the lookups are not requests. GET and DELETE on the
 * endpoint (the earlier versions' event stream and session end) are answered 405.
 */
public class McpMock implements MappingsLoaderExtension, ResponseTransformerV2 {
    static final String NAME = "mcp";
    /** Metadata that marks the endpoint's own stubs, which lookups skip. */
    static final String ENDPOINT_KEY = "axxMcpEndpoint";

    static final String MODERN = "2026-07-28";
    static final List<String> LEGACY = List.of("2025-11-25", "2025-06-18", "2025-03-26");
    static final List<String> VERSIONS = List.of(MODERN, "2025-11-25", "2025-06-18", "2025-03-26");

    static final String META_VERSION = "io.modelcontextprotocol/protocolVersion";
    static final String META_CAPABILITIES = "io.modelcontextprotocol/clientCapabilities";
    static final String META_SERVER = "io.modelcontextprotocol/serverInfo";

    /** The methods of 2026-07-28 that an earlier version does not have. */
    private static final Set<String> MODERN_ONLY = Set.of("server/discover", "subscriptions/listen");
    /** The results clients may cache, which say for how long (not at all: stubs change). */
    private static final Set<String> CACHEABLE = Set.of("server/discover", "tools/list", "resources/list", "resources/templates/list",
            "resources/read", "prompts/list");
    static final Set<String> TOOL_ANSWER_KEYS = Set.of("content", "structuredContent", "isError", "error");

    private static final Logger log = LoggerFactory.getLogger(McpMock.class);

    private final McpSettings settings;
    private final McpServer server;
    private final FileSource files;
    private volatile StubMappings stubs;

    McpMock(McpSettings settings, McpServer server, FileSource files) {
        this.settings = settings;
        this.server = server;
        this.files = files;
    }

    @Override
    public String getName() {
        return NAME;
    }

    @Override
    public boolean applyGlobally() {
        return false;
    }

    /** Adds the endpoint's stubs, at startup and whenever WireMock resets to its mappings. */
    @Override
    public void loadMappingsInto(StubMappings stubMappings) {
        this.stubs = stubMappings;
        stubMappings.addMapping(endpoint(RequestMethod.POST,
                ResponseDefinitionBuilder.responseDefinition().withStatus(200).withTransformers(NAME).build()));
        for (RequestMethod m : List.of(RequestMethod.GET, RequestMethod.DELETE)) {
            stubMappings.addMapping(endpoint(m, ResponseDefinitionBuilder.responseDefinition().withStatus(405).withHeader("Allow", "POST").build()));
        }
    }

    private StubMapping endpoint(RequestMethod method, ResponseDefinition response) {
        StubMapping s = new StubMapping(newRequestPattern(method, urlPathEqualTo(settings.path())).build(), response);
        s.setId(UUID.nameUUIDFromBytes(("axx-mcp:" + method + ":" + settings.path()).getBytes(StandardCharsets.UTF_8)));
        s.setName("MCP server: " + server.source);
        s.setPriority(10);
        s.setMetadata(Metadata.metadata().attr(ENDPOINT_KEY, true).build());
        return s;
    }

    @Override
    public Response transform(Response response, ServeEvent serveEvent) {
        Reply r;
        try {
            r = handle(serveEvent.getRequest());
        } catch (RuntimeException e) {
            log.error("the MCP mock of {} failed", server.source, e);
            r = Reply.error(null, new RpcError(-32603, "the MCP mock of " + server.source + " failed: " + e.getMessage(), null, 500));
        }
        List<HttpHeader> headers = new ArrayList<>();
        if (r.body() != null) {
            headers.add(new HttpHeader("Content-Type", "application/json"));
        }
        r.headers().forEach((k, v) -> headers.add(new HttpHeader(k, v)));
        return Response.Builder.like(response)
                .but()
                .status(r.status())
                .headers(new HttpHeaders(headers))
                .body(r.body() == null ? new byte[0] : Values.write(r.body()).getBytes(StandardCharsets.UTF_8))
                .build();
    }

    /** The answer to a POST to the endpoint. */
    Reply handle(Request request) {
        Object parsed;
        try {
            parsed = Values.parse(request.getBodyAsString());
        } catch (IllegalArgumentException e) {
            return Reply.error(null, new RpcError(-32700, "Parse error: the body is not JSON: " + e.getMessage(), null, 400));
        }
        Map<String, Object> message = Values.map(parsed);
        if (message == null) {
            return Reply.error(null, new RpcError(-32600, "Invalid Request: the body is one JSON-RPC message, an object", null, 400));
        }
        Object id = message.get("id");
        String method = Values.string(message.get("method"));
        if (method == null) {
            if (message.containsKey("result") || message.containsKey("error")) {
                return Reply.accepted();
            }
            return Reply.error(id, new RpcError(-32600, "Invalid Request: a JSON-RPC request has a method", null, 400));
        }
        if (!message.containsKey("id")) {
            // A notification: notifications/initialized, notifications/cancelled...
            return Reply.accepted();
        }
        Object p = message.get("params");
        Map<String, Object> params = p == null ? new LinkedHashMap<>() : Values.map(p);
        try {
            if (params == null) {
                throw new RpcError(-32602, "Invalid params: the params of " + method + " are an object", null, 400);
            }
            Map<String, Object> meta = Values.map(params.get("_meta"));
            String version = meta == null ? null : Values.string(meta.get(META_VERSION));
            if (method.equals("initialize")) {
                return initialize(id, params);
            }
            if (version == null) {
                if (MODERN_ONLY.contains(method)) {
                    throw new RpcError(-32602, "Invalid params: " + method + " carries the protocol version in params._meta[\"" + META_VERSION + "\"]",
                            null, 400);
                }
                return Reply.result(id, legacy(method, params));
            }
            if (!MODERN.equals(version)) {
                throw new RpcError(-32022, "Unsupported protocol version: " + version + " (this server speaks " + String.join(", ", VERSIONS)
                        + "; the versions before " + MODERN + " start with initialize)", Values.obj("supported", VERSIONS, "requested", version), 400);
            }
            String header = request.getHeader("MCP-Protocol-Version");
            if (header != null && !header.equals(version)) {
                throw new RpcError(-32020, "Header mismatch: MCP-Protocol-Version header value '" + header + "' does not match body value '" + version + "'",
                        null, 400);
            }
            if (!(meta.get(META_CAPABILITIES) instanceof Map)) {
                throw new RpcError(-32602, "Invalid params: params._meta has no \"" + META_CAPABILITIES + "\": every request of MCP " + MODERN
                        + " carries its protocol version and the client's capabilities", null, 400);
            }
            return Reply.result(id, modern(method, params));
        } catch (RpcError e) {
            return Reply.error(id, e);
        }
    }

    /** The handshake of the versions before 2026-07-28, with a session id the mock does not check. */
    private Reply initialize(Object id, Map<String, Object> params) {
        String requested = Values.string(params.get("protocolVersion"));
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("protocolVersion", requested != null && LEGACY.contains(requested) ? requested : LEGACY.get(0));
        result.put("capabilities", server.capabilities());
        result.put("serverInfo", server.serverInfo);
        if (server.instructions != null) {
            result.put("instructions", server.instructions);
        }
        Reply r = Reply.result(id, result);
        return new Reply(r.status(), r.body(), Map.of("Mcp-Session-Id", UUID.randomUUID().toString()));
    }

    private Map<String, Object> legacy(String method, Map<String, Object> params) {
        if (method.equals("ping")) {
            return new LinkedHashMap<>();
        }
        return answer(method, params, false);
    }

    /** A result of 2026-07-28: complete, with the server's identity, and caching hints where MCP asks for them. */
    private Map<String, Object> modern(String method, Map<String, Object> params) {
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("resultType", "complete");
        if (method.equals("server/discover")) {
            result.put("supportedVersions", VERSIONS);
            result.put("capabilities", server.capabilities());
            if (server.instructions != null) {
                result.put("instructions", server.instructions);
            }
        } else {
            result.putAll(answer(method, params, true));
        }
        if (CACHEABLE.contains(method)) {
            result.put("ttlMs", 0);
            result.put("cacheScope", "private");
        }
        result.put("_meta", Values.obj(META_SERVER, server.serverInfo));
        return result;
    }

    private Map<String, Object> answer(String method, Map<String, Object> params, boolean modern) {
        return switch (method) {
            case "tools/list" -> Values.obj("tools", server.toolList());
            case "tools/call" -> callTool(params);
            case "resources/list" -> Values.obj("resources", server.resourceList());
            case "resources/templates/list" -> Values.obj("resourceTemplates", server.templateList());
            case "resources/read" -> readResource(params, modern);
            case "prompts/list" -> Values.obj("prompts", server.promptList());
            case "prompts/get" -> getPrompt(params);
            default -> throw new RpcError(-32601, "Method not found: " + method + " (the MCP mock answers "
                    + (modern ? "server/discover" : "initialize, ping") + ", tools/list, tools/call, resources/list, resources/templates/list, "
                    + "resources/read, prompts/list and prompts/get)", null, modern ? 404 : 200);
        };
    }

    private Map<String, Object> callTool(Map<String, Object> params) {
        String name = Values.string(params.get("name"));
        if (name == null) {
            throw new RpcError(-32602, "Invalid params: tools/call names its tool in params.name", null, 200);
        }
        McpServer.Tool tool = server.tool(name);
        if (tool == null) {
            throw new RpcError(-32602, "Unknown tool: " + name, null, 200);
        }
        Map<String, Object> arguments = arguments(params, "tools/call");
        List<String> problems = tool.input().problems(arguments);
        if (!problems.isEmpty()) {
            return Values.obj("content", List.of(text("Invalid arguments for the tool " + name + ": " + String.join("; ", problems))), "isError", true);
        }
        StubMapping stub = stub(toolPath(name), arguments)
                .orElseThrow(() -> noStub("no stub answers the tool " + name + " with these arguments: " + Values.write(arguments)));
        Map<String, Object> answer = answerOf(stub, "the tool " + name);
        String problem = toolAnswerProblem(answer);
        if (problem != null) {
            throw new RpcError(-32603, "the stub " + InProcessStubs.describe(stub) + " of the tool " + name + " is wrong: " + problem, null, 200);
        }
        stubbedError(answer);
        boolean isError = Boolean.TRUE.equals(answer.get("isError"));
        boolean structured = answer.containsKey("structuredContent");
        if (tool.output() != null && !isError) {
            if (!structured) {
                throw new RpcError(-32603, "the stub " + InProcessStubs.describe(stub) + " of the tool " + name
                        + " answers no structuredContent, which the tool's outputSchema describes: answer it, or \"isError\": true", null, 200);
            }
            List<String> wrong = tool.output().problems(answer.get("structuredContent"));
            if (!wrong.isEmpty()) {
                throw new RpcError(-32603, "the stub " + InProcessStubs.describe(stub) + " of the tool " + name
                        + " answers structuredContent that does not match the tool's outputSchema: " + String.join("; ", wrong), null, 200);
            }
        }
        Map<String, Object> result = new LinkedHashMap<>();
        Object content = answer.get("content");
        if (content == null) {
            content = structured ? List.of(text(Values.write(answer.get("structuredContent")))) : List.of();
        }
        result.put("content", content);
        if (structured) {
            result.put("structuredContent", answer.get("structuredContent"));
        }
        if (answer.containsKey("isError")) {
            result.put("isError", isError);
        }
        return result;
    }

    private Map<String, Object> readResource(Map<String, Object> params, boolean modern) {
        String uri = Values.string(params.get("uri"));
        if (uri == null) {
            throw new RpcError(-32602, "Invalid params: resources/read names its resource in params.uri", null, 200);
        }
        Optional<StubMapping> stub = stub(settings.stubsBase() + "/resources", Values.obj("uri", uri));
        if (stub.isPresent()) {
            Map<String, Object> answer = answerOf(stub.get(), "the resource " + uri);
            stubbedError(answer);
            List<Object> contents = Values.list(answer.get("contents"));
            if (contents == null || !keys(answer, Set.of("contents"))) {
                throw new RpcError(-32603, "the stub " + InProcessStubs.describe(stub.get()) + " of the resource " + uri
                        + " is wrong: it answers {\"contents\": [...]}, or an error", null, 200);
            }
            List<Object> out = new ArrayList<>();
            for (Object o : contents) {
                Map<String, Object> c = Values.map(o);
                if (c == null) {
                    throw new RpcError(-32603, "the stub " + InProcessStubs.describe(stub.get()) + " of the resource " + uri
                            + " is wrong: each of its contents is an object with text or blob", null, 200);
                }
                Map<String, Object> item = new LinkedHashMap<>();
                item.put("uri", uri);
                item.putAll(c);
                out.add(item);
            }
            return Values.obj("contents", out);
        }
        McpServer.Resource r = server.resource(uri);
        if (r != null && (r.text() != null || r.blob() != null)) {
            Map<String, Object> item = new LinkedHashMap<>();
            item.put("uri", uri);
            if (r.definition().get("mimeType") != null) {
                item.put("mimeType", r.definition().get("mimeType"));
            }
            item.put(r.text() != null ? "text" : "blob", r.text() != null ? r.text() : r.blob());
            return Values.obj("contents", List.of(item));
        }
        // 2026-07-28 answers a resource that does not exist with Invalid Params; the versions
        // before it, with -32002.
        throw new RpcError(modern ? -32602 : -32002, "Resource not found: " + uri, Values.obj("uri", uri), 200);
    }

    private Map<String, Object> getPrompt(Map<String, Object> params) {
        String name = Values.string(params.get("name"));
        if (name == null) {
            throw new RpcError(-32602, "Invalid params: prompts/get names its prompt in params.name", null, 200);
        }
        McpServer.Prompt prompt = server.prompt(name);
        if (prompt == null) {
            throw new RpcError(-32602, "Unknown prompt: " + name, null, 200);
        }
        Map<String, Object> arguments = arguments(params, "prompts/get");
        List<String> missing = prompt.required().stream().filter(a -> arguments.get(a) == null).toList();
        if (!missing.isEmpty()) {
            throw new RpcError(-32602, "Missing required argument" + (missing.size() > 1 ? "s" : "") + " of the prompt " + name + ": "
                    + String.join(", ", missing), null, 200);
        }
        StubMapping stub = stub(settings.stubsBase() + "/prompts/" + InProcessStubs.segment(name), arguments)
                .orElseThrow(() -> noStub("no stub answers the prompt " + name + " with these arguments: " + Values.write(arguments)));
        Map<String, Object> answer = answerOf(stub, "the prompt " + name);
        stubbedError(answer);
        List<Object> messages = Values.list(answer.get("messages"));
        if (messages == null || !keys(answer, Set.of("description", "messages"))) {
            throw new RpcError(-32603, "the stub " + InProcessStubs.describe(stub) + " of the prompt " + name
                    + " is wrong: it answers {\"description\": ..., \"messages\": [...]}, or an error", null, 200);
        }
        Map<String, Object> result = new LinkedHashMap<>();
        if (answer.get("description") != null) {
            result.put("description", answer.get("description"));
        }
        result.put("messages", messages);
        return result;
    }

    private static Map<String, Object> arguments(Map<String, Object> params, String method) {
        Object a = params.get("arguments");
        if (a == null) {
            return new LinkedHashMap<>();
        }
        Map<String, Object> m = Values.map(a);
        if (m == null) {
            throw new RpcError(-32602, "Invalid params: the arguments of " + method + " are an object", null, 200);
        }
        return m;
    }

    String toolPath(String tool) {
        return settings.stubsBase() + "/tools/" + InProcessStubs.segment(tool);
    }

    private Optional<StubMapping> stub(String path, Object body) {
        return InProcessStubs.find(stubs, path, Values.write(body), ENDPOINT_KEY);
    }

    private RpcError noStub(String message) {
        log.warn("the MCP mock of {}: {}", server.source, message);
        return new RpcError(-32603, message, null, 200);
    }

    /** A stub's JSON body, as an object. */
    private Map<String, Object> answerOf(StubMapping stub, String what) {
        String text;
        try {
            text = InProcessStubs.body(stub.getResponse(), files);
        } catch (RuntimeException e) {
            throw new RpcError(-32603, "the stub " + InProcessStubs.describe(stub) + " of " + what + " has no body: " + e.getMessage(), null, 200);
        }
        Map<String, Object> answer = null;
        try {
            answer = text == null || text.isBlank() ? null : Values.map(Values.parse(text));
        } catch (IllegalArgumentException e) {
            // not JSON: said below
        }
        if (answer == null) {
            throw new RpcError(-32603, "the stub " + InProcessStubs.describe(stub) + " of " + what + " answers a JSON object, not: " + text, null, 200);
        }
        return answer;
    }

    /** A stub's error, answered as a JSON-RPC error. */
    private static void stubbedError(Map<String, Object> answer) {
        if (answer.get("error") == null) {
            return;
        }
        String problem = errorProblem(answer.get("error"));
        if (problem != null) {
            throw new RpcError(-32603, "a stub's error is wrong: " + problem, null, 200);
        }
        Map<String, Object> e = Values.map(answer.get("error"));
        throw new RpcError(((Number) e.get("code")).intValue(), (String) e.get("message"), e.get("data"), 200);
    }

    /** What is wrong with a tool stub's answer, or null. */
    static String toolAnswerProblem(Map<String, Object> answer) {
        for (String k : answer.keySet()) {
            if (!TOOL_ANSWER_KEYS.contains(k)) {
                return "a tool's answer has no key \"" + k + "\": its keys are content, structuredContent and isError, or error";
            }
        }
        if (answer.get("error") != null) {
            if (answer.size() > 1) {
                return "a tool's answer is a result (content, structuredContent, isError) or an error, not both";
            }
            return errorProblem(answer.get("error"));
        }
        if (answer.get("content") != null) {
            List<Object> content = Values.list(answer.get("content"));
            if (content == null) {
                return "a tool's content is a list of content blocks, such as {\"type\": \"text\", \"text\": \"...\"}";
            }
            for (Object o : content) {
                Map<String, Object> block = Values.map(o);
                if (block == null || Values.string(block.get("type")) == null) {
                    return "each content block of a tool's answer has a type (text, image, audio, resource_link or resource): " + Values.write(o);
                }
            }
        }
        if (answer.get("isError") != null && !(answer.get("isError") instanceof Boolean)) {
            return "a tool's isError is true or false";
        }
        return null;
    }

    private static String errorProblem(Object error) {
        Map<String, Object> e = Values.map(error);
        if (e == null || !(e.get("code") instanceof Integer || e.get("code") instanceof Long) || !(e.get("message") instanceof String)) {
            return "an error is {\"code\": <a whole number>, \"message\": \"...\", \"data\": ...}, not " + Values.write(error);
        }
        for (String k : e.keySet()) {
            if (!Set.of("code", "message", "data").contains(k)) {
                return "an error has no key \"" + k + "\": its keys are code, message and data";
            }
        }
        return null;
    }

    private static boolean keys(Map<String, Object> answer, Set<String> allowed) {
        return allowed.containsAll(answer.keySet());
    }

    private static Map<String, Object> text(String text) {
        return Values.obj("type", "text", "text", text);
    }

    /** A JSON-RPC error, with the HTTP status that carries it. */
    static final class RpcError extends RuntimeException {
        private static final long serialVersionUID = 1L;

        final int code;
        final transient Object data;
        final int status;

        RpcError(int code, String message, Object data, int status) {
            super(message);
            this.code = code;
            this.data = data;
            this.status = status;
        }
    }

    /** An answer: its HTTP status, its JSON body (none for 202) and its other headers. */
    record Reply(int status, Object body, Map<String, String> headers) {
        static Reply result(Object id, Object result) {
            return new Reply(200, Values.obj("jsonrpc", "2.0", "id", id, "result", result), Map.of());
        }

        static Reply error(Object id, RpcError e) {
            Map<String, Object> error = Values.obj("code", e.code, "message", e.getMessage());
            if (e.data != null) {
                error.put("data", e.data);
            }
            return new Reply(e.status, Values.obj("jsonrpc", "2.0", "id", id, "error", error), Map.of());
        }

        static Reply accepted() {
            return new Reply(202, null, Map.of());
        }
    }
}
