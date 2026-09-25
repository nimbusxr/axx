// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import com.github.tomakehurst.wiremock.admin.Router;
import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.common.Metadata;
import com.github.tomakehurst.wiremock.extension.AdminApiExtension;
import com.github.tomakehurst.wiremock.extension.ResponseTransformerV2;
import com.github.tomakehurst.wiremock.http.HttpHeader;
import com.github.tomakehurst.wiremock.http.HttpHeaders;
import com.github.tomakehurst.wiremock.http.RequestMethod;
import com.github.tomakehurst.wiremock.http.Response;
import com.github.tomakehurst.wiremock.http.ResponseDefinition;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.stream.Collectors;

/**
 * A WireMock extension that validates each served request and its stubbed response against the
 * mocked dependency's OpenAPI specification: the consumer side of the contract (does the caller
 * use the dependency correctly, and does the mock answer as the real one would).
 *
 * <p>A stub is validated when its metadata names a spec ({@code openApiSpecSource}, a file path or
 * URL), or when {@code OPENAPI_SPEC_SOURCE} sets a default and the stub does not opt out ({@code
 * "openApiValidation": false}). Every finding is recorded on the request journal as an {@value
 * #SUB_EVENT} sub-event (findings format {@value #FORMAT}), which axx reads. In {@code fail} mode a
 * response with an ERROR finding becomes an HTTP 500 with a problem+json body; in {@code report}
 * mode the stub's response is served unchanged. See {@link Settings} for the configuration.
 *
 * <p>The extension also adds {@code GET /__admin/openapi-validation} (its version, findings format
 * and settings) and {@code POST /__admin/openapi-validation/reset} (forget cached specs).
 *
 * <p>It registers itself through WireMock's {@link java.util.ServiceLoader} scanning, so putting
 * the jar on the classpath of WireMock standalone is enough.
 */
public class OpenApiValidatorExtension implements ResponseTransformerV2, AdminApiExtension {
    static final String NAME = "open-api-validator";
    static final String SUB_EVENT = "openapi-validation";
    static final int FORMAT = 1;

    static final String SPEC_SOURCE_KEY = "openApiSpecSource";
    static final String ENABLED_KEY = "openApiValidation";
    static final String LEVELS_KEY = "openApiValidationLevels";
    static final String MODE_KEY = "openApiValidationMode";

    private static final Logger log = LoggerFactory.getLogger(OpenApiValidatorExtension.class);

    static {
        // Scoped to the spec parser's own downloads, not the whole JVM.
        if (Settings.trustAllCerts(System.getenv())) {
            System.setProperty("io.swagger.v3.parser.util.RemoteUrl.trustAll", "true");
        }
    }

    private final Settings settings;
    private final SpecCache specs;

    /** Creates the extension from the environment; WireMock instantiates it reflectively. */
    public OpenApiValidatorExtension() {
        this(Settings.fromEnv(System.getenv()));
    }

    OpenApiValidatorExtension(Settings settings) {
        this.settings = settings;
        this.specs =
                new SpecCache(
                        source -> OpenApiValidator.load(source, settings.authHeader()),
                        System::currentTimeMillis);
    }

    @Override
    public String getName() {
        return NAME;
    }

    @Override
    public Response transform(Response response, ServeEvent serveEvent) {
        StubMapping stub = serveEvent.getStubMapping();
        Metadata metadata = stub == null ? null : stub.getMetadata();
        Optional<String> source = specSource(metadata);
        if (source.isEmpty()) {
            log.debug("no OpenAPI spec for {}; not validated", describe(serveEvent));
            return response;
        }
        String spec = source.get();

        Settings.Mode mode = settings.mode();
        List<Finding> findings;
        try {
            // The stub's levels win over the defaults as a whole: a key a stub sets, however
            // broad, is not overridden by a more specific default.
            Levels stubLevels = stubLevels(metadata);
            mode = stubMode(metadata).orElse(mode);
            findings =
                    OpenApiValidator.findings(specs.get(spec), serveEvent.getRequest(), response)
                            .stream()
                            .map(
                                    f ->
                                            f.withLevel(
                                                    stubLevels
                                                            .find(f.key())
                                                            .or(() -> settings.levels().find(f.key()))
                                                            .orElse(Level.ERROR)))
                            .toList();
        } catch (Exception e) {
            findings =
                    List.of(
                            new Finding(
                                    "validation.spec",
                                    Level.ERROR,
                                    "spec",
                                    "cannot use the OpenAPI spec " + spec + ": " + e.getMessage(),
                                    List.of()));
        }
        serveEvent.appendSubEvent(SUB_EVENT, record(spec, mode, findings));
        logFindings(serveEvent, spec, findings);

        if (mode == Settings.Mode.FAIL && findings.stream().anyMatch(f -> f.level() == Level.ERROR)) {
            return Response.Builder.like(response)
                    .status(500)
                    .headers(new HttpHeaders(new HttpHeader("Content-Type", "application/problem+json")))
                    .body(Json.write(problem(serveEvent, spec, findings)))
                    .build();
        }
        return response;
    }

    @Override
    public void contributeAdminApiRoutes(Router router) {
        router.add(
                RequestMethod.GET,
                "/openapi-validation",
                (admin, serveEvent, pathParams) -> ResponseDefinition.okForJson(info()));
        router.add(
                RequestMethod.POST,
                "/openapi-validation/reset",
                (admin, serveEvent, pathParams) ->
                        ResponseDefinition.okForJson(Map.of("cleared", specs.reset())));
    }

    /** What axx (or anyone) needs to know about this extension. */
    Map<String, Object> info() {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("name", "axx-wiremock-openapi");
        m.put("version", version());
        m.put("format", FORMAT);
        m.put("mode", settings.mode().label());
        m.put("levels", settings.levels().asMap());
        settings.defaultSpec().ifPresent(s -> m.put("defaultSpec", s));
        return m;
    }

    static String version() {
        String v = OpenApiValidatorExtension.class.getPackage().getImplementationVersion();
        return v == null ? "dev" : v;
    }

    private Optional<String> specSource(Metadata metadata) {
        if (metadata != null && metadata.containsKey(SPEC_SOURCE_KEY)) {
            return Optional.of(metadata.getString(SPEC_SOURCE_KEY));
        }
        if (metadata != null
                && metadata.containsKey(ENABLED_KEY)
                && !Boolean.TRUE.equals(metadata.getBoolean(ENABLED_KEY))) {
            return Optional.empty();
        }
        return settings.defaultSpec();
    }

    private static Levels stubLevels(Metadata metadata) {
        if (metadata == null || !metadata.containsKey(LEVELS_KEY)) {
            return Levels.NONE;
        }
        if (!(metadata.get(LEVELS_KEY) instanceof Map<?, ?> map)) {
            throw new IllegalArgumentException(
                    LEVELS_KEY + " must be an object of validation keys and levels");
        }
        return Levels.of(map);
    }

    private static Optional<Settings.Mode> stubMode(Metadata metadata) {
        if (metadata == null || !metadata.containsKey(MODE_KEY)) {
            return Optional.empty();
        }
        return Optional.of(Settings.Mode.parse(metadata.getString(MODE_KEY)));
    }

    private static Map<String, Object> record(String spec, Settings.Mode mode, List<Finding> findings) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("format", FORMAT);
        m.put("extension", version());
        m.put("spec", spec);
        m.put("mode", mode.label());
        m.put("findings", findings.stream().map(Finding::toMap).toList());
        return m;
    }

    private static Map<String, Object> problem(ServeEvent serveEvent, String spec, List<Finding> findings) {
        List<Finding> shown = findings.stream().filter(f -> f.level() != Level.IGNORE).toList();
        long errors = shown.stream().filter(f -> f.level() == Level.ERROR).count();
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("type", "about:blank");
        m.put("title", "OpenAPI validation failed");
        m.put("status", 500);
        m.put(
                "detail",
                describe(serveEvent) + " does not match " + spec + ": " + errors
                        + (errors == 1 ? " error" : " errors"));
        m.put("spec", spec);
        m.put("findings", shown.stream().map(Finding::toMap).toList());
        return m;
    }

    private static void logFindings(ServeEvent serveEvent, String spec, List<Finding> findings) {
        List<Finding> errors = findings.stream().filter(f -> f.level() == Level.ERROR).toList();
        List<Finding> warnings = findings.stream().filter(f -> f.level() == Level.WARN).toList();
        if (!errors.isEmpty()) {
            log.warn("OpenAPI contract violation: {} ({}):\n{}", describe(serveEvent), spec, lines(errors));
        }
        if (!warnings.isEmpty()) {
            log.info("OpenAPI contract warnings: {} ({}):\n{}", describe(serveEvent), spec, lines(warnings));
        }
    }

    private static String lines(List<Finding> findings) {
        return findings.stream()
                .map(f -> "  " + f.key() + " (" + f.side() + "): " + f.message())
                .collect(Collectors.joining("\n"));
    }

    private static String describe(ServeEvent serveEvent) {
        String request = serveEvent.getRequest().getMethod() + " " + serveEvent.getRequest().getUrl();
        StubMapping stub = serveEvent.getStubMapping();
        return stub != null && stub.getName() != null ? request + " [stub " + stub.getName() + "]" : request;
    }
}
