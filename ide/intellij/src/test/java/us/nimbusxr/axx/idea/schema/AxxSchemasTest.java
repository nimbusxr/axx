// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import org.junit.jupiter.params.provider.ValueSource;

class AxxSchemasTest {
    @ParameterizedTest
    @CsvSource({
        "axx.yaml, axx.schema.json",
        "axx.yml, axx.schema.json",
        "axx.local.yaml, axx.schema.json",
        "axx.ci.yml, axx.schema.json",
        "registrations.factory.yaml, axx-factory.schema.json",
        "registrations.factory.yml, axx-factory.schema.json",
        "parcel.fixture.yaml, axx-fixture.schema.json",
        "registrations.prototype.yaml, axx-prototype.schema.json",
    })
    void findsTheSchemaOfAFile(String file, String schema) {
        AxxSchemas.Schema found = AxxSchemas.of(file);
        assertNotNull(found, file);
        assertEquals(schema, found.file());
    }

    @ParameterizedTest
    @ValueSource(strings = {"axx-packs.yaml", "compose.yaml", "my-axx.yaml", "axx.yaml.bak", "axx.a.b.yaml", "factory.yaml"})
    void leavesOtherFilesAlone(String file) {
        assertNull(AxxSchemas.of(file), file);
    }

    @Test
    void bundlesEverySchema() {
        for (AxxSchemas.Schema schema : AxxSchemas.ALL) {
            assertNotNull(AxxSchemas.class.getResource(schema.resource()), schema.resource());
            assertNotNull(AxxSchemas.class.getResource(schema.siteResource()), schema.siteResource());
        }
    }
}
