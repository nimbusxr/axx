// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import com.intellij.lang.annotation.HighlightSeverity;
import com.intellij.testFramework.fixtures.BasePlatformTestCase;

import org.jetbrains.yaml.schema.YamlJsonSchemaHighlightingInspection;

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

    /**
     * A profile's file says only what differs from axx.yaml, which axx lays it under: a service it
     * switches off has its command there, and the file is not told that it lacks one.
     */
    public void testAProfilesOverlayThatSwitchesAServiceOffLacksNothing() {
        myFixture.enableInspections(new YamlJsonSchemaHighlightingInspection());
        String switchedOff = "services:\n  kafka:\n    enabled: false\n";
        myFixture.configureByText("axx.apollo.yaml", switchedOff);
        assertEquals(List.of(), problems());
        // In axx.yaml itself, a service has a command.
        myFixture.configureByText("axx.yaml", switchedOff);
        assertTrue(problems().toString(), problems().toString().contains("command"));
        // And an overlay is still held to axx.yaml's keys.
        myFixture.configureByText("axx.apollo.yaml", "services:\n  kafka:\n    enabld: false\n");
        assertTrue(problems().toString(), problems().toString().contains("enabld"));
    }

    private List<String> problems() {
        return myFixture.doHighlighting(HighlightSeverity.WEAK_WARNING).stream()
                .map(info -> String.valueOf(info.getDescription()))
                .toList();
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
