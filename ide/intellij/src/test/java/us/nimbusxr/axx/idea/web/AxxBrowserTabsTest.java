// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

@DisplayName("AxxBrowserTabs")
class AxxBrowserTabsTest {

    @Test
    @DisplayName("a tab shows another page under its title, the file staying read-only")
    void anotherPage() {
        var file =
                new AxxBrowserTabs.BrowserFile(
                        AxxBrowserTabs.Tab.TRACE, "Trace: register", "http://127.0.0.1:1/a/register.zip");
        file.url = "http://127.0.0.1:1/b/cancel.zip";
        file.title = "Trace: cancel";
        assertEquals("Trace: cancel", file.getName());
        assertFalse(file.isWritable());
    }
}
