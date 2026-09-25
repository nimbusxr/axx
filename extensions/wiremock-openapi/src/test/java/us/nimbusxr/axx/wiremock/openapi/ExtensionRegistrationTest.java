// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import static com.github.tomakehurst.wiremock.client.WireMock.created;
import static com.github.tomakehurst.wiremock.client.WireMock.post;
import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import static org.assertj.core.api.Assertions.assertThat;

import com.github.tomakehurst.wiremock.WireMockServer;
import com.github.tomakehurst.wiremock.common.Metadata;
import com.github.tomakehurst.wiremock.core.Options;
import com.github.tomakehurst.wiremock.standalone.CommandLineOptions;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.net.http.HttpResponse;
import java.nio.file.Path;

/** How WireMock finds the extension: ServiceLoader scanning, or naming its class. */
class ExtensionRegistrationTest {
    @TempDir Path rootDir;

    private WireMockServer wm;

    @AfterEach
    void tearDown() {
        if (wm != null) {
            wm.stop();
        }
    }

    /** Starts WireMock, stubs a validated endpoint and posts an invalid body to it. */
    private int statusForInvalidRequest(Options options) {
        wm = new WireMockServer(options);
        wm.start();
        wm.stubFor(
                post("/users")
                        .withMetadata(
                                Metadata.metadata()
                                        .attr(
                                                "openApiSpecSource",
                                                OpenApiValidatorExtensionTest
                                                        .JSON_OPENAPI_FILE_PATH)
                                        .build())
                        .willReturn(created()));
        HttpResponse<String> response =
                Http.request(wm.port(), "POST", "/users")
                        .header("Content-Type", "application/json")
                        .body("{}")
                        .send();
        return response.statusCode();
    }

    @Test
    void withoutTheExtensionNothingIsValidated() {
        assertThat(statusForInvalidRequest(options().dynamicPort())).isEqualTo(201);
    }

    @Test
    void registersThroughServiceLoaderScanning() {
        assertThat(statusForInvalidRequest(options().dynamicPort().extensionScanningEnabled(true)))
                .isEqualTo(500);
    }

    @Test
    void scanningAlsoRegistersTheAdminEndpoint() {
        wm = new WireMockServer(options().dynamicPort().extensionScanningEnabled(true));
        wm.start();

        HttpResponse<String> response = Http.request(wm.port(), "GET", "/__admin/openapi-validation").send();

        assertThat(response.statusCode()).isEqualTo(200);
        assertThat(response.body()).contains("\"format\"");
    }

    @Test
    void registersByClassName() {
        assertThat(
                        statusForInvalidRequest(
                                options()
                                        .dynamicPort()
                                        .extensions(OpenApiValidatorExtension.class.getName())))
                .isEqualTo(500);
    }

    @Test
    void classNameWithScanningRegistersOneValidator() {
        // The scanned instance and the named one share the extension name, so one overrides the
        // other instead of failing startup with a duplicate-name error.
        assertThat(
                        statusForInvalidRequest(
                                options()
                                        .dynamicPort()
                                        .extensions(OpenApiValidatorExtension.class.getName())
                                        .extensionScanningEnabled(true)))
                .isEqualTo(500);
    }

    @Test
    void standaloneCommandLineWithoutFlag() {
        CommandLineOptions cli =
                new CommandLineOptions("--port", "0", "--root-dir", rootDir.toString());

        assertThat(statusForInvalidRequest(cli)).isEqualTo(500);
    }
}
