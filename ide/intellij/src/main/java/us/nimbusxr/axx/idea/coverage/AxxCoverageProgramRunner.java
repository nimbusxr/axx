// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.CoverageExecutor;
import com.intellij.coverage.CoverageHelper;
import com.intellij.coverage.CoverageSuite;
import com.intellij.execution.ExecutionException;
import com.intellij.execution.ExecutionResult;
import com.intellij.execution.configurations.RunConfigurationBase;
import com.intellij.execution.configurations.RunProfile;
import com.intellij.execution.configurations.RunProfileState;
import com.intellij.execution.configurations.RunnerSettings;
import com.intellij.execution.configurations.coverage.CoverageEnabledConfiguration;
import com.intellij.execution.process.ProcessEvent;
import com.intellij.execution.process.ProcessListener;
import com.intellij.execution.runners.ExecutionEnvironment;
import com.intellij.execution.runners.GenericProgramRunner;
import com.intellij.execution.runners.RunContentBuilder;
import com.intellij.execution.ui.RunContentDescriptor;
import com.intellij.openapi.util.Key;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.LineBuffer;
import us.nimbusxr.axx.idea.run.AxxRunConfiguration;
import us.nimbusxr.axx.idea.web.IdeAnnouncement;

/**
 * Runs axx run configurations with Run with Coverage: {@code axx run --coverage} (see {@link
 * us.nimbusxr.axx.idea.run.AxxRunState}), whose report, once the run announces it, the platform
 * shows as the run ends.
 */
public final class AxxCoverageProgramRunner extends GenericProgramRunner<RunnerSettings> {
    @Override
    public @NotNull String getRunnerId() {
        return "AxxCoverageRunner";
    }

    @Override
    public boolean canRun(@NotNull String executorId, @NotNull RunProfile profile) {
        return CoverageExecutor.EXECUTOR_ID.equals(executorId) && AxxRunConfiguration.of(profile) != null;
    }

    @Override
    protected @Nullable RunContentDescriptor doExecute(
            @NotNull RunProfileState state, @NotNull ExecutionEnvironment environment)
            throws ExecutionException {
        RunConfigurationBase<?> configuration = (RunConfigurationBase<?>) environment.getRunProfile();
        CoverageEnabledConfiguration coverage = CoverageEnabledConfiguration.getOrCreate(configuration);
        CoverageHelper.resetCoverageSuit(configuration);
        CoverageSuite suite = coverage.getCurrentCoverageSuite();
        ExecutionResult result = state.execute(environment.getExecutor(), this);
        if (result == null) {
            return null;
        }
        if (suite != null && suite.getCoverageDataFileProvider() instanceof AxxCoverageEnabledConfiguration.Report report) {
            result.getProcessHandler().addProcessListener(new Announcements(report));
        }
        CoverageHelper.attachToProcess(configuration, result.getProcessHandler(), environment.getRunnerSettings());
        return new RunContentBuilder(result, environment).showRunContent(environment.getContentToReuse());
    }

    /** Hands the report the run announces to its suite. */
    static final class Announcements implements ProcessListener {
        private final AxxCoverageEnabledConfiguration.Report report;
        private final LineBuffer lines = new LineBuffer();

        Announcements(@NotNull AxxCoverageEnabledConfiguration.Report report) {
            this.report = report;
        }

        @Override
        public synchronized void onTextAvailable(@NotNull ProcessEvent event, @NotNull Key outputType) {
            for (String line : lines.append(event.getText())) {
                IdeAnnouncement announcement = IdeAnnouncement.parse(line);
                if (announcement != null && announcement.kind() == IdeAnnouncement.Kind.COVERAGE) {
                    report.announced(announcement.path());
                }
            }
        }
    }
}
