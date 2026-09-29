// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import java.io.IOException;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * The mocked agent's card (A2A 1.0): served as written, and read for what the mock answers: the
 * interfaces it lists (their binding, and their URL's path, which is where the mock answers them)
 * and the capabilities it declares. A card without what A2A requires of one is refused when
 * WireMock starts.
 */
final class AgentCard {
    /** An interface of the card: where, in which binding and protocol version, the agent answers. */
    record Interface(String binding, String url, String path, String version) {
        static final String JSONRPC = "JSONRPC";
        static final String REST = "HTTP+JSON";
    }

    private static final List<String> REQUIRED = List.of("name", "description", "supportedInterfaces", "version", "capabilities",
            "defaultInputModes", "defaultOutputModes", "skills");

    final String source;
    final String text;
    final String name;
    final List<Interface> interfaces;
    final boolean streaming;
    final boolean extendedAgentCard;

    private AgentCard(String source, String text, String name, List<Interface> interfaces, boolean streaming, boolean extendedAgentCard) {
        this.source = source;
        this.text = text;
        this.name = name;
        this.interfaces = interfaces;
        this.streaming = streaming;
        this.extendedAgentCard = extendedAgentCard;
    }

    /** Reads a card file; IllegalArgumentException says what is wrong with it. */
    static AgentCard load(String source) {
        String text;
        try {
            text = Files.readString(Path.of(source), StandardCharsets.UTF_8);
        } catch (IOException e) {
            throw new IllegalArgumentException("cannot read the agent card " + source + " (A2A_AGENT_CARD_SOURCE): " + e, e);
        }
        try {
            return read(source, text);
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("the agent card " + source + " (A2A_AGENT_CARD_SOURCE) is wrong: " + e.getMessage(), e);
        }
    }

    static AgentCard read(String source, String text) {
        Map<String, Object> card;
        try {
            card = Values.map(Values.parse(text));
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("it is not JSON: " + e.getMessage(), e);
        }
        if (card == null) {
            throw new IllegalArgumentException("it is a JSON object, an A2A 1.0 AgentCard");
        }
        for (String k : REQUIRED) {
            if (card.get(k) == null) {
                throw new IllegalArgumentException("it has no " + k + ": an A2A 1.0 card has " + String.join(", ", REQUIRED));
            }
        }
        List<Object> list = Values.list(card.get("supportedInterfaces"));
        if (list == null || list.isEmpty()) {
            throw new IllegalArgumentException("its supportedInterfaces list where the agent answers: "
                    + "[{\"url\": \"http://carrier-agent:8080/a2a\", \"protocolBinding\": \"JSONRPC\", \"protocolVersion\": \"1.0\"}]");
        }
        List<Interface> interfaces = new ArrayList<>();
        for (Object o : list) {
            Map<String, Object> i = Values.map(o);
            String url = i == null ? null : Values.text(i.get("url"));
            String binding = i == null ? null : Values.text(i.get("protocolBinding"));
            String version = i == null ? null : Values.text(i.get("protocolVersion"));
            if (url == null || binding == null || version == null) {
                throw new IllegalArgumentException("each of its supportedInterfaces has a url, a protocolBinding and a protocolVersion: " + Values.write(o));
            }
            String path;
            try {
                path = URI.create(url).getRawPath();
            } catch (IllegalArgumentException e) {
                throw new IllegalArgumentException("the url of an interface is a URL, not " + url, e);
            }
            if (path == null) {
                path = "";
            }
            if (path.endsWith("/")) {
                path = path.substring(0, path.length() - 1);
            }
            String tenant = Values.text(i.get("tenant"));
            if (tenant != null) {
                path = path + "/" + tenant;
            }
            interfaces.add(new Interface(binding, url, path, version));
        }
        Map<String, Object> capabilities = Values.map(card.get("capabilities"));
        if (capabilities == null) {
            throw new IllegalArgumentException("its capabilities are an object, such as {\"streaming\": true}");
        }
        return new AgentCard(source, text, String.valueOf(card.get("name")), interfaces,
                Boolean.TRUE.equals(capabilities.get("streaming")), Boolean.TRUE.equals(capabilities.get("extendedAgentCard")));
    }
}
