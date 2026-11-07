// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.List;
import java.util.regex.Pattern;

/**
 * Which of axx's JSON Schemas governs a YAML file, by the file's name, as axx itself finds them:
 * {@code axx.yaml}, its {@code axx.<name>.yaml} overlays, and the fixture spec files of {@code axx
 * fixtures}.
 *
 * <p>It holds no IDE types, so its rules are unit-testable.
 */
final class AxxSchemas {
    /**
     * One of axx's schemas: its kind, as {@code axx schema} names it, and the file the plugin
     * bundles it in, in {@code /schemas}, built from the repository's schemas/v0.
     */
    record Schema(
            @NotNull String title,
            @NotNull String kind,
            @NotNull String file,
            @NotNull Pattern names) {
        @NotNull String resource() {
            return "/schemas/" + file;
        }

        /** The same schema, whose {@code $id} is the address the docs site publishes it at. */
        @NotNull String siteResource() {
            return "/schemas/site/" + file;
        }
    }

    static final List<Schema> ALL =
            List.of(
                    new Schema(
                            "axx.yaml",
                            "config",
                            "axx.schema.json",
                            Pattern.compile("axx(\\.[^.]+)?\\.ya?ml")),
                    new Schema(
                            "axx fixture factory",
                            "factory",
                            "axx-factory.schema.json",
                            Pattern.compile(".+\\.factory\\.ya?ml")),
                    new Schema(
                            "axx fixture",
                            "fixture",
                            "axx-fixture.schema.json",
                            Pattern.compile(".+\\.fixture\\.ya?ml")),
                    new Schema(
                            "axx fixture prototype",
                            "prototype",
                            "axx-prototype.schema.json",
                            Pattern.compile(".+\\.prototype\\.ya?ml")));

    private AxxSchemas() {}

    /**
     * The schema of a file.
     *
     * @param fileName the file's name, without its directory
     * @return its schema, or null when none of axx's governs it
     */
    static @Nullable Schema of(@NotNull String fileName) {
        for (Schema schema : ALL) {
            if (schema.names().matcher(fileName).matches()) {
                return schema;
            }
        }
        return null;
    }
}
