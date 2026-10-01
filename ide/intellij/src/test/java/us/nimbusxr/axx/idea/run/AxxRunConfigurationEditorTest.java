// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.openapi.util.Disposer;
import com.intellij.testFramework.HeavyPlatformTestCase;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

/** The profiles in the settings of an axx run configuration. */
public class AxxRunConfigurationEditorTest extends HeavyPlatformTestCase {
    private Path suite;

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        suite = Files.createDirectories(Path.of(getProject().getBasePath()).resolve("acceptance"));
        Files.writeString(
                suite.resolve("axx.yaml"),
                "version: 1\nprofiles:\n  web: {}\n  ios: {}\n  android: {}\n");
        Files.writeString(suite.resolve("axx.watch.yaml"), "run: {watch: true}\n");
    }

    private AxxRunConfiguration configuration() {
        AxxRunConfiguration configuration =
                (AxxRunConfiguration)
                        AxxRunConfigurationType.getInstance()
                                .getFactory()
                                .createTemplateConfiguration(getProject());
        configuration.setWorkingDirectory(suite.toString());
        return configuration;
    }

    public void testOffersTheProjectsProfiles() {
        assertEquals(List.of("web", "ios", "android", "watch"), configuration().availableProfiles());
    }

    public void testKeepsTheChosenProfilesInTheirOrder() {
        AxxRunConfiguration configuration = configuration();
        configuration.setProfiles(List.of("watch", "ios"));
        AxxRunConfigurationEditor editor = new AxxRunConfigurationEditor(getProject());
        try {
            editor.resetFrom(configuration);
            AxxRunConfiguration applied = configuration();
            editor.applyTo(applied);
            assertEquals(List.of("watch", "ios"), applied.getProfiles());
        } catch (Exception e) {
            throw new AssertionError(e);
        } finally {
            Disposer.dispose(editor);
        }
    }
}
