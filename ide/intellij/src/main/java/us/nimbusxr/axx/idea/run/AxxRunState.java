// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.DefaultExecutionResult;
import com.intellij.execution.ExecutionException;
import com.intellij.execution.ExecutionResult;
import com.intellij.execution.Executor;
import com.intellij.execution.configurations.GeneralCommandLine;
import com.intellij.execution.configurations.RunProfileState;
import com.intellij.execution.executors.DefaultDebugExecutor;
import com.intellij.execution.process.KillableColoredProcessHandler;
import com.intellij.execution.process.ProcessHandler;
import com.intellij.execution.process.ProcessTerminatedListener;
import com.intellij.execution.runners.ExecutionEnvironment;
import com.intellij.execution.runners.ProgramRunner;
import com.intellij.execution.testframework.autotest.ToggleAutoTestAction;
import com.intellij.execution.testframework.sm.SMTestRunnerConnectionUtil;
import com.intellij.execution.testframework.sm.runner.ui.SMTRunnerConsoleView;
import com.intellij.execution.testframework.ui.BaseTestsOutputConsoleView;
import com.intellij.execution.ui.ConsoleViewContentType;
import com.intellij.util.execution.ParametersListUtil;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxBinary;
import us.nimbusxr.axx.idea.AxxProfiles;
import us.nimbusxr.axx.idea.AxxSettings;

import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * Runs {@code axx run --format teamcity} and shows the scenarios in the test runner. axx's
 * TeamCity service messages build the tree: features, their scenarios (one per outline example),
 * and their steps as tests. {@code AXX_IDE} is set, so packs print what the IDE can show: the
 * traces and videos scenarios keep, and the scenarios that pause. Watch shows the web pack's
 * browsers; Debug pauses its scenarios where they fail and before the steps that have a breakpoint.
 */
final class AxxRunState implements RunProfileState {
    /** The variable that tells axx an IDE runs it, and which. */
    static final String IDE_VARIABLE = "AXX_IDE";

    private final AxxRunConfiguration configuration;
    private final ExecutionEnvironment environment;
    private final @Nullable List<String> targets;

    /**
     * @param targets the targets to run instead of the configuration's (relative to its working
     *     directory), as when rerunning failed scenarios; null for the configuration's
     */
    AxxRunState(
            @NotNull AxxRunConfiguration configuration,
            @NotNull ExecutionEnvironment environment,
            @Nullable List<String> targets) {
        this.configuration = configuration;
        this.environment = environment;
        this.targets = targets;
    }

    /**
     * The command line: {@code <axx> run --format teamcity [--debug-steps=<port>] [--workers 1]
     * [<watch arguments>] [<debug arguments>] <targets> [--profile <profiles>] <arguments>} (see {@link
     * RunTargets#runArguments}).
     */
    @NotNull GeneralCommandLine commandLine() throws ExecutionException {
        Path workingDirectory = configuration.workingDirectory();
        // A relative "axx executable" setting starts at the project's directory, as it does
        // for the language server, not at the run's (the folder of the nearest axx.yaml).
        String basePath = configuration.getProject().getBasePath();
        Path executable =
                AxxBinary.find(basePath != null ? Path.of(basePath) : workingDirectory, "run axx");
        List<String> run = targets != null ? targets : configuration.targetsFor(workingDirectory);
        List<String> pauseAt =
                debugs()
                        ? RunTargets.pauseAt(
                                AxxStepBreakpoints.enabled(configuration.getProject()),
                                run,
                                workingDirectory)
                        : null;
        List<String> extra = new ArrayList<>();
        List<String> profiles = configuration.getProfiles();
        if (!profiles.isEmpty()) {
            extra.addAll(List.of("--profile", AxxProfiles.join(profiles)));
        }
        extra.addAll(ParametersListUtil.parse(configuration.getArguments()));
        List<String> args =
                RunTargets.runArguments(
                        run,
                        extra,
                        environment.getUserData(AxxDebugRunner.STEPS_PORT),
                        watchesBrowsers()
                                ? AxxSettings.getInstance().getWatchSlowdownMillis()
                                : null,
                        pauseAt);
        return new GeneralCommandLine(executable.toString())
                .withParameters(args)
                .withEnvironment(IDE_VARIABLE, "intellij")
                .withWorkingDirectory(workingDirectory)
                .withCharset(StandardCharsets.UTF_8);
    }

    /** Whether the run watches the browsers: it runs with Watch, or its configuration says so. */
    private boolean watchesBrowsers() {
        return AxxWatchExecutor.ID.equals(environment.getExecutor().getId())
                || configuration.isWatchBrowsers();
    }

    /**
     * Whether the run debugs: it runs with Debug, not Run or Watch. It then pauses where a scenario
     * fails and before the steps with a breakpoint (see {@link AxxStepBreakpoints}).
     */
    private boolean debugs() {
        return DefaultDebugExecutor.EXECUTOR_ID.equals(environment.getExecutor().getId());
    }

    @Override
    public @NotNull ExecutionResult execute(
            @NotNull Executor executor, @NotNull ProgramRunner<?> runner)
            throws ExecutionException {
        GeneralCommandLine commandLine = commandLine();
        ProcessHandler handler = new KillableColoredProcessHandler(commandLine);
        ProcessTerminatedListener.attach(handler);

        AxxTestConsoleProperties properties =
                new AxxTestConsoleProperties(
                        configuration, executor, commandLine.getWorkDirectory().toPath());
        BaseTestsOutputConsoleView console =
                SMTestRunnerConnectionUtil.createAndAttachConsole(
                        AxxTestConsoleProperties.FRAMEWORK, handler, properties);
        String notice = environment.getUserData(AxxDebugRunner.NOTICE);
        if (notice != null) {
            console.print(notice + "\n", ConsoleViewContentType.SYSTEM_OUTPUT);
        }

        DefaultExecutionResult result = new DefaultExecutionResult(console, handler);
        AxxRerunFailedTestsAction rerunFailed =
                (AxxRerunFailedTestsAction) properties.createRerunFailedTestsAction(console);
        if (console instanceof SMTRunnerConsoleView smConsole) {
            rerunFailed.setModelProvider(smConsole::getResultsViewer);
        }
        result.setRestartActions(rerunFailed, new ToggleAutoTestAction());
        return result;
    }
}
