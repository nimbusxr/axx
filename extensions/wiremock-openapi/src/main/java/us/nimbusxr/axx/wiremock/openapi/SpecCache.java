// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import com.atlassian.oai.validator.OpenApiInteractionValidator;

import java.net.URI;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.concurrent.ConcurrentHashMap;
import java.util.function.Function;
import java.util.function.LongSupplier;

/**
 * One validator per spec source: parsing a spec is expensive. A spec file is loaded again when it
 * changes (its modification time), and a spec that fails to load is retried after {@link
 * #RETRY_MILLIS}, not on every request. Specs from URLs stay cached until {@link #reset()}.
 */
final class SpecCache {
    static final long RETRY_MILLIS = 5_000;

    private record Entry(
            OpenApiInteractionValidator validator, RuntimeException failure, long stamp, long loadedAt) {}

    private final ConcurrentHashMap<String, Entry> entries = new ConcurrentHashMap<>();
    private final Function<String, OpenApiInteractionValidator> loader;
    private final LongSupplier clock;

    SpecCache(Function<String, OpenApiInteractionValidator> loader, LongSupplier clock) {
        this.loader = loader;
        this.clock = clock;
    }

    /** The validator for a source; throws the load failure if the spec cannot be used. */
    OpenApiInteractionValidator get(String source) {
        long stamp = stamp(source);
        // compute holds the key while loading, so concurrent requests load a spec once.
        Entry e =
                entries.compute(
                        source,
                        (k, old) -> {
                            long now = clock.getAsLong();
                            if (old != null
                                    && old.stamp == stamp
                                    && (old.failure == null || now - old.loadedAt < RETRY_MILLIS)) {
                                return old;
                            }
                            try {
                                return new Entry(loader.apply(k), null, stamp, now);
                            } catch (RuntimeException ex) {
                                return new Entry(null, ex, stamp, now);
                            }
                        });
        if (e.failure != null) {
            throw e.failure;
        }
        return e.validator;
    }

    /** Forgets every spec, so each is loaded again when next used. */
    int reset() {
        int n = entries.size();
        entries.clear();
        return n;
    }

    /** The file's modification time, or -1 for URLs and missing files. */
    private static long stamp(String source) {
        try {
            Path path;
            if (source.startsWith("file:")) {
                path = Path.of(URI.create(source));
            } else if (source.contains("://")) {
                return -1;
            } else {
                path = Path.of(source);
            }
            return Files.getLastModifiedTime(path).toMillis();
        } catch (Exception e) {
            return -1;
        }
    }
}
