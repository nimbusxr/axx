// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.IdeLine;

import java.net.URI;
import java.net.URISyntaxException;
import java.nio.file.InvalidPathException;
import java.nio.file.Path;
import java.util.Set;

/**
 * What packs print for the IDE when {@code AXX_IDE} is set and a scenario pauses (before a step
 * with a breakpoint, or at the step that failed) and resumes:
 *
 * <pre>
 * [AXX-IDE] paused url=http://127.0.0.1:53211/3f9c/ location=features/shop.feature:24 recording=/work/parcels/.axx/web/recording.txt
 * [AXX-IDE] resumed url=http://127.0.0.1:53211/3f9c/
 * </pre>
 *
 * <p>The lines follow the rules of {@link IdeLine}; spaces in values are written {@code %20}. Both
 * need {@code url}: where the paused scenario answers ({@code highlight}, {@code run} and {@code
 * recorded} below it, see {@link PausedScenarioClient}), an {@code http} URL on the loopback
 * interface that ends with {@code /}. {@code paused} may add the {@code location} of the step it
 * paused at (its feature file, relative to the directory of {@code axx.yaml}, and line) and the
 * absolute path of the file where Playwright's Inspector writes the steps it records ({@code
 * recording}).
 *
 * @param paused whether the scenario paused ({@code paused}) or resumed ({@code resumed})
 * @param url where the paused scenario answers, ending with {@code /}
 * @param location where it paused, such as {@code features/shop.feature:24}; null when not given
 * @param recording the file of the steps recorded; null when not given
 */
public record ScenarioPause(
        boolean paused, @NotNull String url, @Nullable String location, @Nullable Path recording) {

    private static final String PAUSED = "paused";
    private static final String RESUMED = "resumed";
    private static final Set<String> LOOPBACK = Set.of("127.0.0.1", "localhost", "[::1]");

    /**
     * Parses a line of console output.
     *
     * @return the pause or resume, or null when the text holds no well-formed one
     */
    public static @Nullable ScenarioPause parse(@Nullable String text) {
        IdeLine line = IdeLine.parse(text);
        if (line == null || !(line.kind().equals(PAUSED) || line.kind().equals(RESUMED))) {
            return null;
        }
        String url = value(line, "url");
        if (url == null || !isLoopbackUrl(url)) {
            return null;
        }
        boolean paused = line.kind().equals(PAUSED);
        return new ScenarioPause(
                paused,
                url,
                paused ? value(line, "location") : null,
                paused ? absolutePath(value(line, "recording")) : null);
    }

    /**
     * Whether a URL is one the IDE sends a paused scenario's requests to: {@code http}, on the
     * loopback interface, ending with {@code /}.
     */
    public static boolean isLoopbackUrl(@NotNull String url) {
        try {
            URI uri = new URI(url);
            return "http".equals(uri.getScheme())
                    && uri.getHost() != null
                    && LOOPBACK.contains(uri.getHost())
                    && uri.getRawUserInfo() == null
                    && uri.getRawPath() != null
                    && uri.getRawPath().endsWith("/")
                    && uri.getRawQuery() == null
                    && uri.getRawFragment() == null;
        } catch (URISyntaxException e) {
            return false;
        }
    }

    private static @Nullable String value(IdeLine line, String key) {
        String value = line.field(key);
        return value == null ? null : value.replace("%20", " ");
    }

    private static @Nullable Path absolutePath(@Nullable String value) {
        if (value == null) {
            return null;
        }
        try {
            Path path = Path.of(value);
            return path.isAbsolute() ? path.normalize() : null;
        } catch (InvalidPathException e) {
            return null;
        }
    }
}
