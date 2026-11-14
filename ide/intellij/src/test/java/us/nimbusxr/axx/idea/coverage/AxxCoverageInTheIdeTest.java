// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.CoverageDataManager;
import com.intellij.coverage.CoverageEngine;
import com.intellij.coverage.CoverageRunner;
import com.intellij.coverage.CoverageSuite;
import com.intellij.coverage.CoverageSuitesBundle;
import com.intellij.coverage.DefaultCoverageFileProvider;
import com.intellij.execution.configurations.RunConfiguration;
import com.intellij.execution.configurations.RunConfigurationBase;
import com.intellij.openapi.util.io.NioFiles;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.rt.coverage.data.ClassData;
import com.intellij.rt.coverage.data.LineData;
import com.intellij.rt.coverage.data.ProjectData;
import com.intellij.testFramework.PsiTestUtil;
import com.intellij.testFramework.fixtures.BasePlatformTestCase;

import us.nimbusxr.axx.idea.run.AxxRunConfigurationType;

import java.nio.file.Files;
import java.nio.file.Path;

/**
 * A run with coverage in the IDE: its coverage knows the run it is of, and the project view says of
 * files and folders what the report does, the files of every language counted folder by folder.
 */
public class AxxCoverageInTheIdeTest extends BasePlatformTestCase {
    private final ProjectData report = new ProjectData();
    private CoverageSuitesBundle run;

    @Override
    protected void tearDown() throws Exception {
        try {
            // The project is the next test's too: forget this one's coverage.
            AxxCoverageAnnotator.getInstance(getProject()).onSuiteChosen(null);
        } catch (Throwable e) {
            addSuppressedException(e);
        } finally {
            super.tearDown();
        }
    }

    public void testAFoldersCoverageIsThatOfItsFilesOfEveryLanguage() {
        VirtualFile orders = file("services/orders/src/orders.kt");
        VirtualFile routes = file("services/orders/src/routes.kt");
        VirtualFile billing = file("services/billing/main.go");
        VirtualFile rates = file("services/billing/rates/rates.go");
        VirtualFile readme = file("services/billing/readme.md");
        VirtualFile guide = file("docs/guide.md");
        lines(orders, 3, 0, 2, 1);
        lines(routes, 0);
        lines(billing, 1, 1);
        lines(rates, 0, 0, 0, 0);

        showTheRun();

        assertEquals("75% lines covered", shownOf(orders));
        assertEquals("100% lines covered", shownOf(billing));
        assertEquals("0% lines covered", shownOf(rates));
        assertNull("a file the report does not name", shownOf(readme));
        VirtualFile services = billing.getParent().getParent();
        assertEquals("50% files, 60% lines covered", shownOf(orders.getParent().getParent()));
        assertEquals("50% files, 33% lines covered", shownOf(billing.getParent()));
        assertEquals("0% files, 0% lines covered", shownOf(rates.getParent()));
        assertEquals("50% files, 45% lines covered", shownOf(services));
        assertEquals("50% files, 45% lines covered", shownOf(services.getParent()));
        assertNull("a folder the report names no file of", shownOf(guide.getParent()));
    }

    public void testAFolderOfTestsIsLeftOut() {
        VirtualFile orders = file("services/orders/src/orders.kt");
        VirtualFile check = file("services/orders/test/orders_test.kt");
        lines(orders, 1, 0);
        lines(check, 1, 1, 1);
        PsiTestUtil.addSourceRoot(getModule(), check.getParent(), true);
        try {
            showTheRun();

            assertNull(shownOf(check.getParent()));
            assertEquals("100% files, 50% lines covered", shownOf(orders.getParent().getParent()));
        } finally {
            PsiTestUtil.removeSourceRoot(getModule(), check.getParent());
        }
    }

    public void testARunsCoverageKnowsItsRunConfiguration() {
        RunConfiguration configuration =
                AxxRunConfigurationType.getInstance()
                        .getFactory()
                        .createTemplateConfiguration(getProject());
        AxxCoverageEngine engine = CoverageEngine.EP_NAME.findExtensionOrFail(AxxCoverageEngine.class);
        assertTrue(engine.isApplicableTo((RunConfigurationBase<?>) configuration));

        // As the platform asks for it when a run with coverage ends.
        CoverageSuite suite =
                engine.createCoverageSuite(
                        engine.createCoverageEnabledConfiguration(
                                (RunConfigurationBase<?>) configuration));

        assertInstanceOf(suite, AxxCoverageSuite.class);
        assertSame(configuration, ((AxxCoverageSuite) suite).getConfiguration());
        assertInstanceOf(suite.getRunner(), AxxCoverageRunner.class);
    }

    public void testARunsCoverageIsItsReport() throws Exception {
        Path project = Files.createTempDirectory("axx-coverage");
        try {
            Files.writeString(project.resolve("axx.yaml"), "services: {}\n");
            Path lcov = Files.createDirectories(project.resolve(".axx/coverage")).resolve("lcov.info");
            assertNull("a run that wrote no report", coverageOf(lcov));

            // A file's records are merged: the report of a run in parts names a file in each.
            Files.writeString(
                    lcov,
                    String.join(
                            "\n",
                            "TN:",
                            "SF:services/orders/src/orders.kt",
                            "FN:3,cancel",
                            "DA:3,2",
                            "DA:4,0",
                            "end_of_record",
                            "SF:services/orders/src/orders.kt",
                            "DA:3,1",
                            "DA:7,5",
                            "end_of_record",
                            ""));
            ProjectData coverage = coverageOf(lcov);
            assertNotNull(coverage);
            ClassData orders =
                    coverage.getClassData(project.resolve("services/orders/src/orders.kt").toString());
            assertNotNull("the files: " + coverage.getClasses().keySet(), orders);
            assertEquals(3, ((LineData) orders.getLineData(3)).getHits());
            assertEquals(0, ((LineData) orders.getLineData(4)).getHits());
            assertEquals(5, ((LineData) orders.getLineData(7)).getHits());

            Files.writeString(lcov, "DA:3,2\nend_of_record\n");
            assertNull("a report that names no file for its lines", coverageOf(lcov));
        } finally {
            NioFiles.deleteRecursively(project);
        }
    }

    /** The coverage the platform gets of a run whose report is at {@code lcov}. */
    private ProjectData coverageOf(Path lcov) {
        return new AxxCoverageSuite(
                        "a run",
                        getProject(),
                        CoverageRunner.getInstance(AxxCoverageRunner.class),
                        new DefaultCoverageFileProvider(lcov.toString()),
                        0)
                .getCoverageData(CoverageDataManager.getInstance(getProject()));
    }

    /** Shows the report as the coverage of a run that ended. */
    private void showTheRun() {
        AxxCoverageSuite suite =
                new AxxCoverageSuite(
                        "a run",
                        getProject(),
                        CoverageRunner.getInstance(AxxCoverageRunner.class),
                        new DefaultCoverageFileProvider("lcov.info"),
                        0);
        suite.setCoverageData(report);
        run = new CoverageSuitesBundle(suite);
        AxxCoverageAnnotator.getInstance(getProject())
                .renewCoverageData(run, CoverageDataManager.getInstance(getProject()));
    }

    /** What the project view says next to a file or a folder. */
    private String shownOf(VirtualFile file) {
        AxxCoverageAnnotator annotator = AxxCoverageAnnotator.getInstance(getProject());
        CoverageDataManager manager = CoverageDataManager.getInstance(getProject());
        return file.isDirectory()
                ? annotator.getDirCoverageInformationString(getProject(), file, run, manager)
                : annotator.getFileCoverageInformationString(getProject(), file, run, manager);
    }

    // Lowercase names: the platform looks a file up by its path in lowercase where the file
    // system does not tell cases apart.
    private VirtualFile file(String path) {
        return myFixture.addFileToProject(path, "\n\n\n\n\n").getVirtualFile();
    }

    /** The file's lines in the report, from line 1, each with its hits. */
    private void lines(VirtualFile file, int... hits) {
        ClassData data = report.getOrCreateClassData(file.getPath());
        LineData[] lines = new LineData[hits.length + 1];
        for (int i = 0; i < hits.length; i++) {
            lines[i + 1] = new LineData(i + 1, null);
            lines[i + 1].setHits(hits[i]);
        }
        data.setLines(lines);
    }
}
