// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.json;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

class JsonObjectsTest {
    @Test
    void keysKeepTheirOrderAndNulls() {
        assertThat(JsonText.write(JsonObjects.of("reference", "PX-A2A-9206", "refusal", null, "status", "IN_TRANSIT")))
                .isEqualTo("{\"reference\":\"PX-A2A-9206\",\"refusal\":null,\"status\":\"IN_TRANSIT\"}");
    }

    @Test
    void aKeyWithoutItsValueIsRefused() {
        assertThatThrownBy(() -> JsonObjects.of("reference", "PX-A2A-9206", "status"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("3 is odd");
    }
}
