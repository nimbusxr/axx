// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpRequest.BodyPublishers;
import java.net.http.HttpResponse;
import java.net.http.HttpResponse.BodyHandlers;
import java.time.Duration;

/** A minimal HTTP/1.1 client for the tests. */
final class Http {
    private static final HttpClient CLIENT =
            HttpClient.newBuilder()
                    .version(HttpClient.Version.HTTP_1_1)
                    .connectTimeout(Duration.ofSeconds(5))
                    .build();

    private final HttpRequest.Builder builder;
    private final String method;
    private String body;

    private Http(int port, String method, String path) {
        this.builder = HttpRequest.newBuilder(URI.create("http://localhost:" + port + path));
        this.method = method;
    }

    static Http request(int port, String method, String path) {
        return new Http(port, method, path);
    }

    Http header(String name, String value) {
        builder.header(name, value);
        return this;
    }

    Http body(String body) {
        this.body = body;
        return this;
    }

    HttpResponse<String> send() {
        HttpRequest.BodyPublisher publisher =
                body == null ? BodyPublishers.noBody() : BodyPublishers.ofString(body);
        try {
            return CLIENT.send(
                    builder.method(method, publisher).timeout(Duration.ofSeconds(30)).build(),
                    BodyHandlers.ofString());
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException(e);
        }
    }
}
