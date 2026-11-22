// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.List;
import java.util.regex.Pattern;

/**
 * Which of axx's JSON Schemas governs a YAML file, by the file's name, as axx itself finds them:
 * {@code axx.yaml}, its {@code axx.<name>.yaml} overlays (a profile's file, and axx.local.yaml),
 * and the fixture spec files of {@code axx fixtures}.
 *
 * <p>An overlay has a schema of its own. axx lays it over axx.yaml and checks the result, so an
 * overlay says only what differs: held to axx.yaml's schema, a profile's file that switches a
 * service off was told it lacked the service's command, which axx.yaml has.
 *
 * <p>It holds no IDE types, so its rules are unit-testable.
 */
final class AxxSchemas {
    /**
     * One of axx's schemas: its kind, as {@code axx schema} names it, and the file the plugin
     * bundles it in, in {@code /schemas}, built from the repository's schemas/v0. A kind axx
     * gained after its first releases ({@code since}) is one an earlier axx cannot give: the
     * plugin's own is used for it then, beside the schemas that axx does give.
     */
    record Schema(
            @NotNull String title,
            @NotNull String kind,
            @NotNull String file,
            @NotNull Pattern names,
            boolean since) {
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
                            Pattern.compile("axx\\.ya?ml"),
                            false),
                    new Schema(
                            "axx.yaml overlay",
                            "overlay",
                            "axx-overlay.schema.json",
                            Pattern.compile("axx\\.[^.]+\\.ya?ml"),
                            true),
                    new Schema(
                            "axx fixture factory",
                            "factory",
                            "axx-factory.schema.json",
                            Pattern.compile(".+\\.factory\\.ya?ml"),
                            false),
                    new Schema(
                            "axx fixture",
                            "fixture",
                            "axx-fixture.schema.json",
                            Pattern.compile(".+\\.fixture\\.ya?ml"),
                            false),
                    new Schema(
                            "axx fixture prototype",
                            "prototype",
                            "axx-prototype.schema.json",
                            Pattern.compile(".+\\.prototype\\.ya?ml"),
                            false));

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
