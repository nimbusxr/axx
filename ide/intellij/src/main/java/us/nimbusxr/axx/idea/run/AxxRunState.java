// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.DefaultExecutionResult;
import com.intellij.execution.ExecutionException;
import com.intellij.execution.ExecutionResult;
import com.intellij.execution.Executor;
import com.intellij.execution.configurations.GeneralCommandLine;
import com.intellij.execution.configurations.RunProfileState;
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

import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.List;

/**
 * Runs {@code axx run --format teamcity} and shows the scenarios in the test runner. axx's
 * TeamCity service messages build the tree: features, their scenarios (one per outline example),
 * and their steps as tests.
 */
final class AxxRunState implements RunProfileState {
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
     * The command line: {@code <axx> run --format teamcity [--debug-steps=<port>] <targets>
     * <arguments>}.
     */
    @NotNull GeneralCommandLine commandLine() throws ExecutionException {
        Path workingDirectory = configuration.workingDirectory();
        Path executable = AxxBinary.find(workingDirectory, "run axx");
        List<String> run = targets != null ? targets : configuration.targetsFor(workingDirectory);
        List<String> args =
                RunTargets.runArguments(
                        run,
                        ParametersListUtil.parse(configuration.getArguments()),
                        environment.getUserData(AxxDebugRunner.STEPS_PORT));
        return new GeneralCommandLine(executable.toString())
                .withParameters(args)
                .withWorkingDirectory(workingDirectory)
                .withCharset(StandardCharsets.UTF_8);
    }

    @Override
    public @NotNull ExecutionResult execute(
            @NotNull Executor executor, @NotNull ProgramRunner<?> runner)
            throws ExecutionException {
        ProcessHandler handler = new KillableColoredProcessHandler(commandLine());
        ProcessTerminatedListener.attach(handler);

        AxxTestConsoleProperties properties = new AxxTestConsoleProperties(configuration, executor);
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
