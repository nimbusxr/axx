// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.Executor;
import com.intellij.execution.testframework.actions.AbstractRerunFailedTestsAction;
import com.intellij.execution.testframework.sm.FileUrlProvider;
import com.intellij.execution.testframework.sm.runner.SMTRunnerConsoleProperties;
import com.intellij.execution.testframework.sm.runner.SMTestLocator;
import com.intellij.execution.ui.ConsoleView;

import org.jetbrains.annotations.NotNull;

/**
 * The test runner settings for axx: axx prints TeamCity service messages that build the tree by
 * node IDs (scenarios run in parallel), and locates nodes with {@code file://} URLs.
 */
final class AxxTestConsoleProperties extends SMTRunnerConsoleProperties {
    /** The test framework name, which the IDE shows and keys its settings by. */
    static final String FRAMEWORK = "axx";

    AxxTestConsoleProperties(
            @NotNull AxxRunConfiguration configuration, @NotNull Executor executor) {
        super(configuration, FRAMEWORK, executor);
        setIdBasedTestTree(true);
    }

    /** Resolves {@code file:///abs/path:line} to that line of the Feature file. */
    @Override
    public @NotNull SMTestLocator getTestLocator() {
        return FileUrlProvider.INSTANCE;
    }

    @Override
    public @NotNull AbstractRerunFailedTestsAction createRerunFailedTestsAction(
            @NotNull ConsoleView consoleView) {
        return new AxxRerunFailedTestsAction(consoleView, this);
    }
}
