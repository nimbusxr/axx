// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.Executor;
import com.intellij.execution.testframework.TestConsoleProperties;
import com.intellij.execution.testframework.actions.AbstractRerunFailedTestsAction;
import com.intellij.execution.testframework.sm.FileUrlProvider;
import com.intellij.execution.testframework.sm.SMCustomMessagesParsing;
import com.intellij.execution.testframework.sm.runner.OutputToGeneralTestEventsConverter;
import com.intellij.execution.testframework.sm.runner.SMTRunnerConsoleProperties;
import com.intellij.execution.testframework.sm.runner.SMTestLocator;
import com.intellij.execution.ui.ConsoleView;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.nio.file.Path;

/**
 * The test runner settings for axx: axx prints TeamCity service messages that build the tree by
 * node IDs (scenarios run in parallel), and locates nodes with {@code file://} URLs. The lines packs
 * print for the IDE are read too (see {@link AxxTestEventsConverter}).
 */
final class AxxTestConsoleProperties extends SMTRunnerConsoleProperties
        implements SMCustomMessagesParsing {
    /** The test framework name, which the IDE shows and keys its settings by. */
    static final String FRAMEWORK = "axx";

    private final @Nullable Path workingDirectory;

    /**
     * @param workingDirectory the directory axx runs in, where the files packs name are, or null
     *     when unknown
     */
    AxxTestConsoleProperties(
            @NotNull AxxRunConfiguration configuration,
            @NotNull Executor executor,
            @Nullable Path workingDirectory) {
        super(configuration, FRAMEWORK, executor);
        this.workingDirectory = workingDirectory;
        setIdBasedTestTree(true);
    }

    /** Resolves {@code file:///abs/path:line} to that line of the Feature file. */
    @Override
    public @NotNull SMTestLocator getTestLocator() {
        return FileUrlProvider.INSTANCE;
    }

    @Override
    public @NotNull OutputToGeneralTestEventsConverter createTestEventsConverter(
            @NotNull String testFrameworkName, @NotNull TestConsoleProperties consoleProperties) {
        return new AxxTestEventsConverter(testFrameworkName, consoleProperties, workingDirectory);
    }

    @Override
    public @NotNull AbstractRerunFailedTestsAction createRerunFailedTestsAction(
            @NotNull ConsoleView consoleView) {
        return new AxxRerunFailedTestsAction(consoleView, this);
    }
}
