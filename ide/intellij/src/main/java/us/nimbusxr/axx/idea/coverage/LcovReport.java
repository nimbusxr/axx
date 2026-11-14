// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.lcov.LcovCoverageReport;
import com.intellij.coverage.lcov.LcovCoverageReport.LineHits;
import com.intellij.coverage.lcov.LcovSerializationUtils;
import com.intellij.openapi.util.SystemInfo;
import com.intellij.openapi.util.io.FileUtil;
import com.intellij.openapi.util.text.StringUtil;
import com.intellij.rt.coverage.data.ProjectData;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.ConfigFinder;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * The lcov report of a run with coverage. It names the files relative to the axx project: the
 * folder of the axx.yaml above the report, or else the run's working directory.
 *
 * <p>The file is read here, line by line as the platform's lcov reader does, and its records are
 * merged and turned into coverage by the platform. The platform's own reading of the file takes a
 * {@code File} up to 2026.1 and a {@code Path} from 2026.2, which deprecates the first: no one call
 * serves every IDE the plugin supports.
 */
final class LcovReport {
    private static final Pattern FUNCTION = Pattern.compile("^FN:(\\d+),(.+)$");

    private LcovReport() {}

    /** The coverage of the lcov report at {@code lcov}, its relative paths those of {@code project}. */
    static @NotNull ProjectData load(@NotNull Path lcov, @NotNull Path project) throws IOException {
        return LcovSerializationUtils.convertToProjectData(
                read(lcov),
                path -> {
                    Path p = Path.of(path);
                    return (p.isAbsolute() ? p : project.resolve(p)).normalize().toString();
                });
    }

    /**
     * The axx project of a report: the folder of the axx.yaml above it (coverage-core's folder is in
     * the project), else the run's working directory, else the report's folder.
     */
    static @NotNull Path projectOf(@NotNull Path lcov, @Nullable Path workingDirectory) {
        Path dir = lcov.toAbsolutePath().getParent();
        Path config = dir == null ? null : ConfigFinder.nearestAbove(dir);
        if (config != null) {
            return config.getParent();
        }
        if (workingDirectory != null) {
            return workingDirectory;
        }
        return dir != null ? dir : lcov;
    }

    /** The report's records: each file's lines and their hits, a file's records merged. */
    private static @NotNull LcovCoverageReport read(@NotNull Path lcov) throws IOException {
        LcovCoverageReport report = new LcovCoverageReport();
        String file = null;
        String function = null;
        List<LineHits> lines = null;
        try (BufferedReader reader =
                new BufferedReader(
                        new InputStreamReader(Files.newInputStream(lcov), StandardCharsets.UTF_8))) {
            for (String line = reader.readLine(); line != null; line = reader.readLine()) {
                if (line.startsWith("SF:")) {
                    file = line.substring("SF:".length());
                    function = null;
                    lines = new ArrayList<>();
                } else if (line.startsWith("FN:")) {
                    Matcher named = FUNCTION.matcher(line);
                    if (named.find()) {
                        function = named.group(2);
                    }
                } else if (line.startsWith("DA:")) {
                    if (lines == null) {
                        throw new IOException("a line's hits before the file they are of: " + line);
                    }
                    String[] hits = line.substring("DA:".length()).split(",");
                    if (hits.length < 2) {
                        throw new IOException("a line without its hits: " + line);
                    }
                    lines.add(
                            new LineHits(
                                    Integer.parseInt(hits[0]), Integer.parseInt(hits[1]), function));
                } else if (line.equals("end_of_record")) {
                    if (lines == null) {
                        throw new IOException("a record's end before the file it is of");
                    }
                    report.mergeFileReport(sameFile(file), lines);
                    file = null;
                    lines = null;
                }
            }
        }
        return report;
    }

    // One name for a file however the report writes it, as the platform's reader has it.
    private static @NotNull String sameFile(@NotNull String path) {
        return FileUtil.toSystemIndependentName(
                SystemInfo.isWindows ? StringUtil.toLowerCase(path) : path);
    }
}
