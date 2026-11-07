// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.util.SystemInfo;
import com.intellij.openapi.util.io.NioFiles;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.testFramework.PlatformTestUtil;
import com.intellij.testFramework.fixtures.BasePlatformTestCase;

import us.nimbusxr.axx.idea.AxxSettings;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.concurrent.TimeUnit;

/**
 * The IDE completes axx.yaml from the schema of the axx the project runs on, whichever release of
 * the plugin is installed: an axx newer than the plugin has keys the plugin's own schema lacks.
 */
public class AxxProjectSchemasTest extends BasePlatformTestCase {
    private Path project;
    private Path calls;
    private String executable;

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        project = Files.createTempDirectory("axx-schemas");
        Files.writeString(project.resolve("axx.yaml"), "version: 1\n");
        calls = project.resolve("calls");
        executable = AxxSettings.getInstance().getExecutable();
    }

    @Override
    protected void tearDown() throws Exception {
        try {
            AxxSettings.getInstance().setExecutable(executable);
            AxxProjectSchemas.getInstance(getProject()).forget();
            PlatformTestUtil.dispatchAllInvocationEventsInIdeEventQueue();
            NioFiles.deleteRecursively(project);
        } finally {
            super.tearDown();
        }
    }

    @Override
    protected boolean shouldRunTest() {
        return super.shouldRunTest() && !SystemInfo.isWindows; // the fake axx is a shell script
    }

    /**
     * An axx that says it is version, and whose axx.yaml schema has the key fromTheProjectsAxx;
     * with schemaExit other than 0, `axx schema` fails.
     */
    private void fakeAxx(String version, int schemaExit) throws Exception {
        String config =
                "{\"$schema\":\"http://json-schema.org/draft-07/schema#\","
                        + "\"$id\":\"" + AxxProjectSchemas.GITHUB + "axx.schema.json\","
                        + "\"type\":\"object\",\"properties\":{\"fromTheProjectsAxx\":{\"type\":\"string\"},"
                        + "\"services\":{\"type\":\"object\"}}}";
        Path axx = project.resolve("axx");
        Files.writeString(
                axx,
                "#!/bin/sh\n"
                        + "echo \"$*\" >> '" + calls + "'\n"
                        + "case \"$1\" in\n"
                        + "  --version) echo 'axx " + version + " (test)' ;;\n"
                        + "  schema)\n"
                        + "    [ " + schemaExit + " -eq 0 ] || exit " + schemaExit + "\n"
                        + "    if [ \"$3\" = config ]; then printf '%s' '" + config + "' > \"$5\";"
                        + " else printf '%s' '{\"type\":\"object\"}' > \"$5\"; fi ;;\n"
                        + "esac\n",
                StandardCharsets.UTF_8);
        assertTrue(axx.toFile().setExecutable(true));
        AxxSettings.getInstance().setExecutable(axx.toString());
    }

    /** Has the project's axx asked for its schemas, as a refresh does, and the IDE take them up. */
    private void update() throws Exception {
        AxxProjectSchemas schemas = AxxProjectSchemas.getInstance(getProject());
        ApplicationManager.getApplication()
                .executeOnPooledThread(() -> schemas.update(project))
                .get(60, TimeUnit.SECONDS);
        PlatformTestUtil.dispatchAllInvocationEventsInIdeEventQueue();
    }

    private List<String> complete(String file, String text) {
        myFixture.configureByText(file, text);
        myFixture.completeBasic();
        List<String> found = myFixture.getLookupElementStrings();
        return found == null ? List.of() : found;
    }

    private List<String> calls() throws Exception {
        return Files.exists(calls) ? Files.readAllLines(calls) : List.of();
    }

    public void testAxxYamlCompletesFromTheAxxTheProjectRunsOn() throws Exception {
        fakeAxx("v9.9.9", 0);
        update();
        List<String> found = complete("axx.yaml", "<caret>");
        assertTrue(found.toString(), found.contains("fromTheProjectsAxx"));
        assertFalse("the plugin's own schema is not in use: " + found, found.contains("run"));
    }

    public void testAFileThatNamesTheSiteCompletesFromItToo() throws Exception {
        fakeAxx("v9.9.9", 0);
        update();
        List<String> found =
                complete(
                        "axx.yaml",
                        "# yaml-language-server: $schema=" + AxxProjectSchemas.SITE + "axx.schema.json\n<caret>");
        assertTrue(found.toString(), found.contains("fromTheProjectsAxx"));
    }

    public void testAnAxxThatCannotGiveItsSchemasLeavesThePluginsOwn() throws Exception {
        fakeAxx("v9.9.9", 1);
        update();
        List<String> found = complete("axx.yaml", "<caret>");
        assertTrue(found.toString(), found.contains("run") && found.contains("services"));
        assertFalse(found.toString(), found.contains("fromTheProjectsAxx"));
    }

    public void testTheSameAxxIsAskedForItsSchemasOnce() throws Exception {
        fakeAxx("v9.9.9", 0);
        update();
        update();
        List<String> schemaCalls = calls().stream().filter(c -> c.startsWith("schema")).toList();
        assertEquals(schemaCalls.toString(), AxxSchemas.ALL.size(), schemaCalls.size());
        assertEquals(2, calls().stream().filter(c -> c.equals("--version")).count());
    }

    /** An upgraded axx, or another version an axx.yaml names, says so in --version: it is asked again. */
    public void testAnotherAxxIsAskedAgain() throws Exception {
        fakeAxx("v9.9.9", 0);
        update();
        fakeAxx("v10.0.0", 0);
        update();
        long schemaCalls = calls().stream().filter(c -> c.startsWith("schema")).count();
        assertEquals(2L * AxxSchemas.ALL.size(), schemaCalls);
        assertTrue(complete("axx.yaml", "<caret>").contains("fromTheProjectsAxx"));
    }

    /**
     * Opt-in: with {@code AXX_BIN} set to an axx binary, the schemas the IDE uses are that axx's own,
     * byte for byte. {@code AXX_BIN=$PWD/../../bin/axx ./gradlew test --tests '*AxxProjectSchemasTest*'}
     */
    public void testARealAxxGivesItsOwnSchemas() throws Exception {
        String bin = System.getenv("AXX_BIN");
        if (bin == null) {
            return;
        }
        AxxSettings.getInstance().setExecutable(bin);
        update();
        for (AxxSchemas.Schema schema : AxxSchemas.ALL) {
            Process p =
                    new ProcessBuilder(bin, "schema", "--kind", schema.kind(), "--full")
                            .directory(project.toFile())
                            .redirectError(ProcessBuilder.Redirect.DISCARD)
                            .start();
            String printed = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
            assertEquals(0, p.waitFor());
            VirtualFile given = AxxProjectSchemas.getInstance(getProject()).file(schema, false);
            assertNotNull(schema.kind(), given);
            assertEquals(schema.kind(), printed, new String(given.contentsToByteArray(), StandardCharsets.UTF_8));
        }
        assertTrue(complete("axx.yaml", "<caret>").contains("hooks"));
    }

    public void testASchemaIsKeptAtTheSitesAddressToo() {
        assertEquals(
                "{\"$id\":\"" + AxxProjectSchemas.SITE + "axx.schema.json\"}",
                AxxProjectSchemas.atSite("{\"$id\":\"" + AxxProjectSchemas.GITHUB + "axx.schema.json\"}"));
    }
}
