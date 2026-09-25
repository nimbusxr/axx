// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.ExecutionException;
import com.intellij.execution.Executor;
import com.intellij.execution.configurations.RunProfileState;
import com.intellij.execution.runners.ExecutionEnvironment;
import com.intellij.execution.testframework.AbstractTestProxy;
import com.intellij.execution.testframework.actions.AbstractRerunFailedTestsAction;
import com.intellij.openapi.ui.ComponentContainer;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;

/**
 * Reruns the scenarios that failed: {@code axx run <file:line>...} with the locations of the
 * failed scenario nodes (the Scenario line, or the Examples row of an outline example).
 */
final class AxxRerunFailedTestsAction extends AbstractRerunFailedTestsAction {
    AxxRerunFailedTestsAction(
            @NotNull ComponentContainer componentContainer,
            @NotNull AxxTestConsoleProperties properties) {
        super(componentContainer);
        init(properties);
    }

    @Override
    protected @Nullable MyRunProfile getRunProfile(@NotNull ExecutionEnvironment environment) {
        AxxRunConfiguration configuration =
                AxxRunConfiguration.of(myConsoleProperties.getConfiguration());
        if (configuration == null) {
            return null;
        }
        Path workingDirectory;
        try {
            workingDirectory = configuration.workingDirectory();
        } catch (ExecutionException e) {
            workingDirectory = null;
        }
        List<String> targets =
                failedScenarioTargets(getFailedTests(configuration.getProject()), workingDirectory);
        if (targets.isEmpty()) {
            return null;
        }
        return new MyRunProfile(configuration) {
            @Override
            public @NotNull RunProfileState getState(
                    @NotNull Executor executor, @NotNull ExecutionEnvironment env) {
                return new AxxRunState(configuration, env, targets);
            }
        };
    }

    /**
     * The targets of the scenarios failed tests belong to. The tree is feature, scenario, step: a
     * failed step reruns its scenario, a failed scenario itself, and a failed feature only when it
     * ran no scenario (for example when it does not parse).
     */
    static @NotNull List<String> failedScenarioTargets(
            @NotNull List<? extends AbstractTestProxy> failed, @Nullable Path workingDirectory) {
        Set<String> targets = new LinkedHashSet<>();
        for (AbstractTestProxy test : failed) {
            AbstractTestProxy scenario = scenarioOf(test);
            String url = scenario == null ? null : scenario.getLocationUrl();
            String target = RunTargets.fromLocationUrl(url, workingDirectory);
            if (target != null) {
                targets.add(target);
            }
        }
        return new ArrayList<>(targets);
    }

    private static @Nullable AbstractTestProxy scenarioOf(AbstractTestProxy test) {
        int depth = 0;
        for (AbstractTestProxy p = test.getParent(); p != null; p = p.getParent()) {
            depth++;
        }
        return switch (depth) {
            case 3 -> test.getParent();
            case 2 -> test;
            case 1 -> test.getChildren().isEmpty() ? test : null;
            default -> null;
        };
    }
}
