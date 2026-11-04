// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import com.intellij.testFramework.fixtures.BasePlatformTestCase;

import java.util.List;

/** What a person gets who opens axx's YAML files, in the IDE's own completion, with no network. */
public class AxxSchemaCompletionTest extends BasePlatformTestCase {
    private List<String> complete(String file, String text) {
        myFixture.configureByText(file, text);
        myFixture.completeBasic();
        List<String> found = myFixture.getLookupElementStrings();
        return found == null ? List.of() : found;
    }

    private void assertOffers(List<String> found, String... keys) {
        for (String key : keys) {
            assertTrue(key + " not in " + found, found.contains(key));
        }
    }

    public void testAxxYamlCompletesItsTopLevelKeys() {
        assertOffers(complete("axx.yaml", "<caret>"), "version", "run", "services", "properties", "profiles");
    }

    public void testAxxYamlCompletesAService() {
        assertOffers(
                complete("axx.yaml", "version: 1\nservices:\n  api:\n    <caret>"),
                "command",
                "ready",
                "cleanup");
    }

    public void testAProfilesOverlayCompletesLikeAxxYaml() {
        assertOffers(complete("axx.ci.yaml", "<caret>"), "run", "services");
    }

    public void testAFactoryCompletesItsKeys() {
        assertOffers(complete("registrations.factory.yaml", "<caret>"), "factory", "defaults", "fixtures");
    }

    public void testAnotherYamlFileGetsNone() {
        List<String> found = complete("compose.yaml", "<caret>");
        assertFalse(found.toString(), found.contains("services") && found.contains("profiles") && found.contains("run"));
    }

    /** A file that names the site, as those `axx init` wrote before do, on a network that blocks it. */
    public void testAFileThatNamesTheOldAddressStillCompletes() {
        assertOffers(
                complete(
                        "axx.yaml",
                        "# yaml-language-server: $schema=https://axx.nimbusxr.us/schemas/v0/axx.schema.json\n<caret>"),
                "run",
                "services");
    }

    public void testAFactoryThatNamesTheOldAddressStillCompletes() {
        assertOffers(
                complete(
                        "registrations.factory.yaml",
                        "# yaml-language-server: $schema=https://axx.nimbusxr.us/schemas/v0/axx-factory.schema.json\n<caret>"),
                "factory",
                "defaults");
    }

    public void testAFileThatNamesGitHubStillCompletes() {
        assertOffers(
                complete(
                        "axx.yaml",
                        "# yaml-language-server: $schema=https://raw.githubusercontent.com/nimbusxr/axx/main/schemas/v0/axx.schema.json\n<caret>"),
                "run",
                "services");
    }
}
