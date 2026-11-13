// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.CoverageFileProvider;
import com.intellij.coverage.CoverageRunner;
import com.intellij.execution.ExecutionException;
import com.intellij.execution.configurations.RunConfigurationBase;
import com.intellij.execution.configurations.coverage.CoverageEnabledConfiguration;

import java.nio.file.Files;
import java.nio.file.Path;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.run.AxxRunConfiguration;

/**
 * An axx run configuration run with coverage. Its coverage is the lcov report the run announces as
 * it ends ({@code [AXX-IDE] coverage path=...}): {@code .axx/coverage/lcov.info} in its axx
 * project, unless coverage-core's {@code folder} puts it elsewhere. A run that announces none
 * (one that stopped before it measured anything) shows none, never an earlier run's report.
 */
final class AxxCoverageEnabledConfiguration extends CoverageEnabledConfiguration {
    AxxCoverageEnabledConfiguration(@NotNull RunConfigurationBase<?> configuration) {
        super(configuration, CoverageRunner.getInstance(AxxCoverageRunner.class));
    }

    /** Where the run's report usually is, until the run announces it. */
    @Override
    protected @Nullable String createCoverageFile() {
        Path dir = workingDirectory();
        return dir == null ? null : dir.resolve(".axx").resolve("coverage").resolve("lcov.info").toString();
    }

    private @Nullable Path workingDirectory() {
        return workingDirectory(getConfiguration());
    }

    /** A run's working directory: its axx project, unless the configuration names another. */
    static @Nullable Path workingDirectory(@NotNull RunConfigurationBase<?> configuration) {
        AxxRunConfiguration axx = AxxRunConfiguration.of(configuration);
        if (axx == null) {
            return null;
        }
        try {
            return axx.workingDirectory();
        } catch (ExecutionException e) {
            return null;
        }
    }

    @Override
    public @NotNull CoverageFileProvider createFileProvider() {
        return new Report(getCoverageFilePath());
    }

    /** The report of one run: the one it announced, once it has. */
    static final class Report implements CoverageFileProvider {
        private final @Nullable String expected;
        private volatile @Nullable Path announced;

        Report(@Nullable String expected) {
            this.expected = expected;
        }

        /** Takes the report the run announced. */
        void announced(@NotNull Path lcov) {
            announced = lcov;
        }

        @Override
        public @Nullable String getCoverageDataFilePath() {
            Path lcov = announced;
            return lcov != null ? lcov.toString() : expected;
        }

        @Override
        public boolean ensureFileExists() {
            Path lcov = announced;
            return lcov != null && Files.isRegularFile(lcov);
        }

        @Override
        public boolean isValid() {
            return ensureFileExists();
        }
    }
}
