// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.CoverageDataManager;
import com.intellij.coverage.CoverageEngine;
import com.intellij.coverage.SimpleCoverageAnnotator;
import com.intellij.openapi.components.Service;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.roots.ProjectFileIndex;
import com.intellij.openapi.roots.TestSourcesFilter;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.rt.coverage.data.ProjectData;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.Map;
import java.util.Set;

/** The coverage of files and folders in the project view, from a run with coverage. */
@Service(Service.Level.PROJECT)
public final class AxxCoverageAnnotator extends SimpleCoverageAnnotator {
    public AxxCoverageAnnotator(@NotNull Project project) {
        super(project);
    }

    static @NotNull AxxCoverageAnnotator getInstance(@NotNull Project project) {
        return project.getService(AxxCoverageAnnotator.class);
    }

    /**
     * A folder's coverage: that of every file under it the report names, of every language.
     *
     * <p>The platform's own walk asks the engine, file by file, whether the file counts, through a
     * method it keeps to itself (CoverageEngine.coverageProjectViewStatisticsApplicableTo, an
     * internal API, whose answer for other plugins is no). So this is that walk with the question
     * left out. A folder of tests is counted as any other when the run tracks tests: the platform
     * keeps its list of those folders to itself too.
     */
    @Override
    protected @Nullable DirCoverageInfo collectFolderCoverage(
            @NotNull VirtualFile dir,
            @NotNull CoverageDataManager dataManager,
            CoverageAnnotatorRunner annotator,
            ProjectData projectInfo,
            boolean trackTestFolders,
            @NotNull ProjectFileIndex index,
            @NotNull CoverageEngine coverageEngine,
            Set<? super VirtualFile> visitedDirs,
            @NotNull Map<String, String> normalizedFiles2Files) {
        if (!visitedDirs.add(dir)) {
            return null;
        }
        VirtualFile[] children =
                dataManager.doInReadActionIfProjectOpen(
                        () -> {
                            if (!index.isInContent(dir) && !index.isInLibrary(dir)) {
                                return null;
                            }
                            if (!trackTestFolders
                                    && TestSourcesFilter.isTestSources(dir, getProject())) {
                                return null;
                            }
                            return dir.getChildren();
                        });
        if (children == null) {
            return null;
        }
        DirCoverageInfo folder = new DirCoverageInfo();
        for (VirtualFile child : children) {
            if (child.isDirectory()) {
                DirCoverageInfo inside =
                        collectFolderCoverage(
                                child,
                                dataManager,
                                annotator,
                                projectInfo,
                                trackTestFolders,
                                index,
                                coverageEngine,
                                visitedDirs,
                                normalizedFiles2Files);
                if (inside != null) {
                    folder.totalFilesCount += inside.totalFilesCount;
                    folder.coveredFilesCount += inside.coveredFilesCount;
                    folder.totalLineCount += inside.totalLineCount;
                    folder.coveredLineCount += inside.coveredLineCount;
                }
                continue;
            }
            FileCoverageInfo file =
                    collectBaseFileCoverage(child, annotator, projectInfo, normalizedFiles2Files);
            if (file != null) {
                folder.totalFilesCount++;
                folder.totalLineCount += file.totalLineCount;
                if (file.coveredLineCount > 0) {
                    folder.coveredFilesCount++;
                    folder.coveredLineCount += file.coveredLineCount;
                }
            }
        }
        if (folder.totalFilesCount == 0) {
            return null;
        }
        annotator.annotateSourceDirectory(normalizeFilePath(dir.getPath()), folder);
        return folder;
    }
}
