// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.intellij.execution.process.NopProcessHandler;
import com.intellij.execution.process.ProcessEvent;
import com.intellij.execution.process.ProcessOutputTypes;
import com.intellij.rt.coverage.data.ClassData;
import com.intellij.rt.coverage.data.LineData;
import com.intellij.rt.coverage.data.ProjectData;

import java.nio.file.Files;
import java.nio.file.Path;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

@DisplayName("Run with Coverage")
class AxxCoverageRunnerTest {
    @TempDir Path project;

    @Test
    @DisplayName("reads the run's lcov report, its relative paths the axx project's")
    void readsTheReport() throws Exception {
        Files.writeString(project.resolve("axx.yaml"), "services: {}\n");
        Path lcov = Files.createDirectories(project.resolve(".axx/coverage")).resolve("lcov.info");
        Path elsewhere = project.resolve("vendor/lib.js").toAbsolutePath();
        Files.writeString(
                lcov,
                String.join(
                        "\n",
                        "TN:",
                        "SF:services/orders/src/main/kotlin/Orders.kt",
                        "DA:3,2",
                        "DA:4,0",
                        "end_of_record",
                        "TN:",
                        "SF:" + elsewhere,
                        "DA:1,1",
                        "end_of_record",
                        ""));

        Path root = LcovReport.projectOf(lcov, null);
        assertEquals(project.toAbsolutePath(), root);
        ProjectData data = LcovReport.load(lcov, root);
        ClassData orders =
                data.getClassData(
                        project.resolve("services/orders/src/main/kotlin/Orders.kt").toString());
        assertNotNull(orders, "the classes: " + data.getClasses().keySet());
        assertEquals(2, ((LineData) orders.getLineData(3)).getHits());
        assertEquals(0, ((LineData) orders.getLineData(4)).getHits());
        assertNotNull(data.getClassData(elsewhere.toString()), "an absolute path stays as it is");
    }

    @Test
    @DisplayName("resolves a report outside any axx project against the run's working directory")
    void reportOutsideTheProject() throws Exception {
        Path lcov = Files.createDirectories(project.resolve("out")).resolve("lcov.info");
        Path work = project.resolve("work");
        assertEquals(work, LcovReport.projectOf(lcov, work));
    }

    @Test
    @DisplayName("takes the report a run announces, in whatever pieces its output comes")
    void takesTheAnnouncedReport() throws Exception {
        Path lcov = Files.createDirectories(project.resolve("cov")).resolve("lcov.info");
        AxxCoverageEnabledConfiguration.Report report =
                new AxxCoverageEnabledConfiguration.Report(project.resolve("expected.info").toString());
        assertFalse(report.ensureFileExists(), "no report until the run announces one");

        AxxCoverageProgramRunner.Announcements listener = new AxxCoverageProgramRunner.Announcements(report);
        NopProcessHandler handler = new NopProcessHandler();
        String line = "[AXX-IDE] coverage path=" + lcov + "\n";
        int half = line.length() / 2;
        listener.onTextAvailable(new ProcessEvent(handler, "coverage: lines 50%\n" + line.substring(0, half)), ProcessOutputTypes.STDOUT);
        listener.onTextAvailable(new ProcessEvent(handler, line.substring(half)), ProcessOutputTypes.STDOUT);
        assertEquals(lcov.toString(), report.getCoverageDataFilePath());
        assertFalse(report.ensureFileExists(), "the announced report is not written yet");
        Files.writeString(lcov, "TN:\n");
        assertTrue(report.ensureFileExists());
    }
}
