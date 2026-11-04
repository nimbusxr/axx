// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.VirtualFile;
import com.jetbrains.jsonSchema.extension.JsonSchemaFileProvider;
import com.jetbrains.jsonSchema.extension.JsonSchemaProviderFactory;
import com.jetbrains.jsonSchema.extension.SchemaType;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.ArrayList;
import java.util.List;

/**
 * Gives axx's YAML files the IDE's completion, documentation and validation from axx's JSON
 * Schemas, which the plugin bundles: nothing to set up, a comment at the top of the file not
 * needed, and no network involved.
 *
 * <p>A comment at the top of a file goes first in the IDE, which downloads the schema it names
 * unless the {@code $id} of a schema it has is that address. The bundled schemas' {@code $id} is
 * their address on GitHub; {@link SiteAddress} has them at the docs site's too, which the files
 * {@code axx init} wrote before name.
 */
public final class AxxSchemaProviderFactory implements JsonSchemaProviderFactory {
    @Override
    public @NotNull List<JsonSchemaFileProvider> getProviders(@NotNull Project project) {
        List<JsonSchemaFileProvider> providers = new ArrayList<>();
        for (AxxSchemas.Schema schema : AxxSchemas.ALL) {
            providers.add(new Provider(schema));
            providers.add(new SiteAddress(schema));
        }
        return providers;
    }

    /** A bundled schema for a file that names the docs site: it maps no file by its name. */
    private record SiteAddress(AxxSchemas.Schema schema) implements JsonSchemaFileProvider {
        @Override
        public boolean isAvailable(@NotNull VirtualFile file) {
            return false;
        }

        @Override
        public @NotNull String getName() {
            return schema.title() + " (the docs site's address)";
        }

        @Override
        public @Nullable VirtualFile getSchemaFile() {
            return JsonSchemaProviderFactory.getResourceFile(
                    AxxSchemaProviderFactory.class, schema.siteResource());
        }

        @Override
        public @NotNull SchemaType getSchemaType() {
            return SchemaType.embeddedSchema;
        }

        @Override
        public boolean isUserVisible() {
            return false;
        }
    }

    private record Provider(AxxSchemas.Schema schema) implements JsonSchemaFileProvider {
        @Override
        public boolean isAvailable(@NotNull VirtualFile file) {
            return AxxSchemas.of(file.getName()) == schema;
        }

        @Override
        public @NotNull String getName() {
            return schema.title();
        }

        @Override
        public @Nullable VirtualFile getSchemaFile() {
            return JsonSchemaProviderFactory.getResourceFile(
                    AxxSchemaProviderFactory.class, schema.resource());
        }

        @Override
        public @NotNull SchemaType getSchemaType() {
            return SchemaType.embeddedSchema;
        }
    }
}
