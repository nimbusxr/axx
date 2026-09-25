// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.atlassian.oai.validator.OpenApiInteractionValidator;

import org.junit.jupiter.api.Test;

import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

class LevelsAndCacheTest {
    @Test
    void theMostSpecificKeyWins() {
        Levels l = Levels.parse("validation.request=WARN, validation.request.body.schema.required=IGNORE");

        assertThat(l.find("validation.request.body.schema.required")).contains(Level.IGNORE);
        assertThat(l.find("validation.request.body.schema.type")).contains(Level.WARN);
        assertThat(l.find("validation.response.body")).isEmpty();
    }

    @Test
    void laterLevelsOverrideEarlierOnes() {
        Levels l = Levels.parse("validation.request=WARN").with(Levels.of(Map.of("validation.request", "error")));

        assertThat(l.find("validation.request.path.missing")).contains(Level.ERROR);
    }

    @Test
    void levelNamesAreCaseInsensitiveAndFailIsError() {
        assertThat(Level.parse("fail")).isEqualTo(Level.ERROR);
        assertThat(Level.parse(" Warn ")).isEqualTo(Level.WARN);
        assertThatThrownBy(() -> Level.parse("LOUD")).hasMessageContaining("supported levels");
        assertThatThrownBy(() -> Levels.parse("validation.request")).hasMessageContaining("key=LEVEL");
    }

    @Test
    void settingsComeFromTheEnvironment() {
        Settings s =
                Settings.fromEnv(
                        Map.of(
                                "OPENAPI_VALIDATION_MODE", "Report",
                                "OPENAPI_VALIDATION_LEVELS", "validation.response=WARN",
                                "OPENAPI_SPEC_SOURCE", "/var/openapi/api.yaml",
                                "OPENAPI_SPEC_AUTH_HEADER", "Authorization: Bearer t0ken"));

        assertThat(s.mode()).isEqualTo(Settings.Mode.REPORT);
        assertThat(s.levels().find("validation.response.body")).contains(Level.WARN);
        assertThat(s.levels().find("validation.request.parameter.query.unexpected")).contains(Level.IGNORE);
        assertThat(s.defaultSpec()).contains("/var/openapi/api.yaml");
        assertThat(s.authHeader().orElseThrow()).containsExactly("Authorization", "Bearer t0ken");
        assertThat(Settings.fromEnv(Map.of()).mode()).isEqualTo(Settings.Mode.FAIL);
        assertThatThrownBy(() -> Settings.fromEnv(Map.of("OPENAPI_VALIDATION_MODE", "loud")))
                .hasMessageContaining("use fail or report");
    }

    @Test
    void aSpecThatFailsToLoadIsRetriedOnlyAfterAWhile() {
        AtomicInteger loads = new AtomicInteger();
        AtomicLong now = new AtomicLong(1_000);
        SpecCache cache =
                new SpecCache(
                        source -> {
                            loads.incrementAndGet();
                            throw new IllegalStateException("cannot load " + source);
                        },
                        now::get);

        assertThatThrownBy(() -> cache.get("https://example.test/api.yaml")).hasMessageContaining("cannot load");
        assertThatThrownBy(() -> cache.get("https://example.test/api.yaml")).hasMessageContaining("cannot load");
        assertThat(loads).hasValue(1);

        now.addAndGet(SpecCache.RETRY_MILLIS);
        assertThatThrownBy(() -> cache.get("https://example.test/api.yaml")).hasMessageContaining("cannot load");
        assertThat(loads).hasValue(2);
    }

    @Test
    void aLoadedSpecIsLoadedOnceUntilReset() {
        AtomicInteger loads = new AtomicInteger();
        SpecCache cache =
                new SpecCache(
                        source -> {
                            loads.incrementAndGet();
                            return OpenApiInteractionValidator.createForSpecificationUrl(source).build();
                        },
                        System::currentTimeMillis);
        String spec = OpenApiValidatorExtensionTest.JSON_OPENAPI_FILE_PATH;

        assertThat(cache.get(spec)).isSameAs(cache.get(spec));
        assertThat(loads).hasValue(1);
        assertThat(cache.reset()).isEqualTo(1);
        cache.get(spec);
        assertThat(loads).hasValue(2);
    }
}
