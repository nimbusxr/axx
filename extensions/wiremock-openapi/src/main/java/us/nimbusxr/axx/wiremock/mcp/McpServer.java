// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * The mocked MCP server as its description file says, in MCP's own field names: its {@code
 * serverInfo} and {@code instructions}, and what it lists: its {@code tools}, {@code resources}
 * (with, optionally, a resource's content inline as {@code text} or {@code blob}), {@code
 * resourceTemplates} and {@code prompts}. The description is what the server lists; stubs are
 * what it answers.
 *
 * <p>A description that is not one (an unknown key, a tool without an input schema, two tools of
 * one name) is refused when WireMock starts, with what is wrong and where.
 */
final class McpServer {
    /** A tool: its definition as listed, and its schemas compiled. */
    record Tool(String name, Map<String, Object> definition, ToolSchema input, ToolSchema output) {}

    /** A resource: its definition as listed (without its content), and its inline content. */
    record Resource(String uri, Map<String, Object> definition, String text, String blob) {}

    /** A prompt: its definition as listed, and the names of its required arguments. */
    record Prompt(String name, Map<String, Object> definition, List<String> required) {}

    private static final Set<String> KEYS = Set.of("serverInfo", "instructions", "tools", "resources", "resourceTemplates", "prompts");

    final String source;
    final Map<String, Object> serverInfo;
    final String instructions;
    private final Map<String, Tool> tools;
    private final Map<String, Resource> resources;
    private final List<Map<String, Object>> templates;
    private final Map<String, Prompt> prompts;

    private McpServer(String source, Map<String, Object> serverInfo, String instructions, Map<String, Tool> tools,
            Map<String, Resource> resources, List<Map<String, Object>> templates, Map<String, Prompt> prompts) {
        this.source = source;
        this.serverInfo = serverInfo;
        this.instructions = instructions;
        this.tools = tools;
        this.resources = resources;
        this.templates = templates;
        this.prompts = prompts;
    }

    /** Reads a description file; IllegalArgumentException says what is wrong with it. */
    static McpServer load(String source) {
        String text;
        try {
            text = Files.readString(Path.of(source), StandardCharsets.UTF_8);
        } catch (IOException e) {
            throw new IllegalArgumentException("cannot read the MCP server description " + source + " (MCP_SERVER_SOURCE): " + e, e);
        }
        try {
            return read(source, text);
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("the MCP server description " + source + " (MCP_SERVER_SOURCE) is wrong: " + e.getMessage(), e);
        }
    }

    static McpServer read(String source, String text) {
        Object parsed;
        try {
            parsed = Values.parseYaml(text);
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("it is not JSON or YAML: " + e.getMessage(), e);
        }
        Map<String, Object> d = Values.map(parsed);
        if (d == null) {
            throw new IllegalArgumentException("it is an object with serverInfo, and tools, resources, resourceTemplates or prompts");
        }
        for (String k : d.keySet()) {
            if (!KEYS.contains(k)) {
                throw new IllegalArgumentException("it has no key \"" + k + "\": its keys are serverInfo, instructions, tools, resources, "
                        + "resourceTemplates and prompts");
            }
        }
        Map<String, Object> info = Values.map(d.get("serverInfo"));
        if (info == null || Values.string(info.get("name")) == null || Values.string(info.get("version")) == null) {
            throw new IllegalArgumentException("its serverInfo names the server and its version: {\"name\": \"carrier-tools\", \"version\": \"1.0.0\"}");
        }
        if (d.get("instructions") != null && !(d.get("instructions") instanceof String)) {
            throw new IllegalArgumentException("its instructions are text");
        }
        Map<String, Tool> tools = new LinkedHashMap<>();
        for (Map<String, Object> t : objects(d, "tools")) {
            String name = required(t, "name", "each tool");
            Map<String, Object> input = Values.map(t.get("inputSchema"));
            if (input == null || !"object".equals(input.get("type"))) {
                throw new IllegalArgumentException("the tool " + name + " has an inputSchema, a JSON Schema of \"type\": \"object\"");
            }
            ToolSchema in = schema(name, "inputSchema", input);
            ToolSchema out = null;
            if (t.get("outputSchema") != null) {
                Map<String, Object> output = Values.map(t.get("outputSchema"));
                if (output == null) {
                    throw new IllegalArgumentException("the tool " + name + "'s outputSchema is a JSON Schema object");
                }
                out = schema(name, "outputSchema", output);
            }
            if (tools.put(name, new Tool(name, t, in, out)) != null) {
                throw new IllegalArgumentException("it has two tools named " + name);
            }
        }
        Map<String, Resource> resources = new LinkedHashMap<>();
        for (Map<String, Object> r : objects(d, "resources")) {
            String uri = required(r, "uri", "each resource");
            required(r, "name", "the resource " + uri);
            String inline = optionalString(r, "text", "the resource " + uri);
            String blob = optionalString(r, "blob", "the resource " + uri);
            if (inline != null && blob != null) {
                throw new IllegalArgumentException("the resource " + uri + " has its content as text or as blob, not both");
            }
            if (blob != null) {
                try {
                    Base64.getDecoder().decode(blob);
                } catch (IllegalArgumentException e) {
                    throw new IllegalArgumentException("the resource " + uri + "'s blob is base64: " + e.getMessage(), e);
                }
            }
            Map<String, Object> definition = new LinkedHashMap<>(r);
            definition.remove("text");
            definition.remove("blob");
            if (resources.put(uri, new Resource(uri, definition, inline, blob)) != null) {
                throw new IllegalArgumentException("it has two resources of the URI " + uri);
            }
        }
        List<Map<String, Object>> templates = new ArrayList<>();
        for (Map<String, Object> t : objects(d, "resourceTemplates")) {
            String template = required(t, "uriTemplate", "each resource template");
            required(t, "name", "the resource template " + template);
            templates.add(t);
        }
        Map<String, Prompt> prompts = new LinkedHashMap<>();
        for (Map<String, Object> p : objects(d, "prompts")) {
            String name = required(p, "name", "each prompt");
            List<String> requiredArgs = new ArrayList<>();
            List<Object> args = p.get("arguments") == null ? List.of() : Values.list(p.get("arguments"));
            if (args == null) {
                throw new IllegalArgumentException("the prompt " + name + "'s arguments are a list of {\"name\", \"description\", \"required\"}");
            }
            for (Object o : args) {
                Map<String, Object> a = Values.map(o);
                if (a == null || Values.string(a.get("name")) == null) {
                    throw new IllegalArgumentException("each argument of the prompt " + name + " has a name");
                }
                if (Boolean.TRUE.equals(a.get("required"))) {
                    requiredArgs.add((String) a.get("name"));
                }
            }
            if (prompts.put(name, new Prompt(name, p, requiredArgs)) != null) {
                throw new IllegalArgumentException("it has two prompts named " + name);
            }
        }
        return new McpServer(source, info, (String) d.get("instructions"), tools, resources, templates, prompts);
    }

    private static ToolSchema schema(String tool, String which, Map<String, Object> schema) {
        try {
            return ToolSchema.of(schema);
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("the tool " + tool + "'s " + which + " is wrong: " + e.getMessage(), e);
        }
    }

    private static List<Map<String, Object>> objects(Map<String, Object> d, String key) {
        if (d.get(key) == null) {
            return List.of();
        }
        List<Object> list = Values.list(d.get(key));
        if (list == null) {
            throw new IllegalArgumentException("its " + key + " are a list");
        }
        List<Map<String, Object>> out = new ArrayList<>();
        for (Object o : list) {
            Map<String, Object> m = Values.map(o);
            if (m == null) {
                throw new IllegalArgumentException("each of its " + key + " is an object, not " + Values.write(o));
            }
            out.add(m);
        }
        return out;
    }

    private static String required(Map<String, Object> m, String key, String what) {
        String v = Values.string(m.get(key));
        if (v == null || v.isEmpty()) {
            throw new IllegalArgumentException(what + " has a " + key + ": " + Values.write(m));
        }
        return v;
    }

    private static String optionalString(Map<String, Object> m, String key, String what) {
        Object v = m.get(key);
        if (v != null && !(v instanceof String)) {
            throw new IllegalArgumentException(what + "'s " + key + " is text");
        }
        return (String) v;
    }

    Tool tool(String name) {
        return tools.get(name);
    }

    Resource resource(String uri) {
        return resources.get(uri);
    }

    Prompt prompt(String name) {
        return prompts.get(name);
    }

    boolean hasTool(String name) {
        return tools.containsKey(name);
    }

    /** The names of the tools, for messages. */
    String toolNames() {
        return tools.isEmpty() ? "none" : String.join(", ", tools.keySet());
    }

    List<Object> toolList() {
        return tools.values().stream().map(t -> (Object) t.definition()).toList();
    }

    List<Object> resourceList() {
        return resources.values().stream().map(r -> (Object) r.definition()).toList();
    }

    List<Object> templateList() {
        return new ArrayList<>(templates);
    }

    List<Object> promptList() {
        return prompts.values().stream().map(p -> (Object) p.definition()).toList();
    }

    /** What the server offers: tools, resources and prompts when it lists any. */
    Map<String, Object> capabilities() {
        Map<String, Object> c = new LinkedHashMap<>();
        if (!tools.isEmpty()) {
            c.put("tools", new LinkedHashMap<>());
        }
        if (!resources.isEmpty() || !templates.isEmpty()) {
            c.put("resources", new LinkedHashMap<>());
        }
        if (!prompts.isEmpty()) {
            c.put("prompts", new LinkedHashMap<>());
        }
        return c;
    }
}
