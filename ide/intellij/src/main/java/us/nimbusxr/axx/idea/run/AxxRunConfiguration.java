// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.ExecutionException;
import com.intellij.execution.Executor;
import com.intellij.execution.configurations.ConfigurationFactory;
import com.intellij.execution.configurations.LocatableConfigurationBase;
import com.intellij.execution.configurations.RunConfiguration;
import com.intellij.execution.configurations.RunProfile;
import com.intellij.execution.configurations.RunProfileState;
import com.intellij.execution.configurations.RuntimeConfigurationError;
import com.intellij.execution.configurations.RuntimeConfigurationException;
import com.intellij.execution.configurations.WrappingRunConfiguration;
import com.intellij.execution.runners.ExecutionEnvironment;
import com.intellij.execution.testframework.sm.runner.SMRunnerConsolePropertiesProvider;
import com.intellij.execution.testframework.sm.runner.SMTRunnerConsoleProperties;
import com.intellij.openapi.options.SettingsEditor;
import com.intellij.openapi.project.Project;
import com.intellij.util.execution.ParametersListUtil;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.ConfigFinder;

import java.nio.file.Files;
import java.nio.file.InvalidPathException;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * An axx run configuration: {@code axx run --format teamcity <targets> <arguments>}, in the axx
 * project's directory, with the scenarios in the IDE's test runner.
 */
public final class AxxRunConfiguration
        extends LocatableConfigurationBase<AxxRunConfigurationOptions>
        implements SMRunnerConsolePropertiesProvider {

    AxxRunConfiguration(
            @NotNull Project project, @NotNull ConfigurationFactory factory, @NotNull String name) {
        super(project, factory, name);
    }

    /** The axx run configuration a run profile runs, or null for another kind of profile. */
    public static @Nullable AxxRunConfiguration of(@Nullable RunProfile profile) {
        if (profile instanceof AxxRunConfiguration configuration) {
            return configuration;
        }
        if (profile instanceof WrappingRunConfiguration<?> wrapping
                && wrapping.getPeer() instanceof AxxRunConfiguration configuration) {
            return configuration;
        }
        return null;
    }

    @Override
    protected @NotNull AxxRunConfigurationOptions getOptions() {
        return (AxxRunConfigurationOptions) super.getOptions();
    }

    @Override
    protected @NotNull Class<AxxRunConfigurationOptions> getDefaultOptionsClass() {
        return AxxRunConfigurationOptions.class;
    }

    /** The targets: feature files or directories, each optionally with {@code :line}. */
    public @NotNull List<String> getTargets() {
        return ParametersListUtil.parse(getOptions().getTargets());
    }

    public void setTargets(@NotNull List<String> targets) {
        getOptions().setTargets(ParametersListUtil.join(targets));
    }

    /** The targets as the settings editor shows them. */
    @NotNull String getTargetsText() {
        return getOptions().getTargets();
    }

    void setTargetsText(@NotNull String targets) {
        getOptions().setTargets(targets.strip());
    }

    /** Extra {@code axx run} arguments, as a command line. */
    public @NotNull String getArguments() {
        return getOptions().getArguments();
    }

    public void setArguments(@NotNull String arguments) {
        getOptions().setArguments(arguments.strip());
    }

    /** The configured working directory; empty for the axx project of the first target. */
    public @NotNull String getWorkingDirectory() {
        return getOptions().getWorkingDirectory();
    }

    public void setWorkingDirectory(@NotNull String workingDirectory) {
        getOptions().setWorkingDirectory(workingDirectory.strip());
    }

    /**
     * The directory axx runs in: the configured one, else the directory of the axx config above the
     * first target, else the project's axx project.
     */
    @NotNull Path workingDirectory() throws ExecutionException {
        Path base = projectDirectory();
        try {
            String configured = getWorkingDirectory();
            if (!configured.isEmpty()) {
                return base.resolve(configured).normalize();
            }
            List<String> targets = getTargets();
            if (!targets.isEmpty()) {
                Path first = base.resolve(RunTargets.pathOf(targets.get(0))).normalize();
                Path dir = Files.isDirectory(first) ? first : first.getParent();
                Path config = ConfigFinder.nearestAbove(dir);
                if (config != null) {
                    return config.getParent();
                }
            }
        } catch (InvalidPathException e) {
            throw new ExecutionException(
                    "Invalid path in the axx run configuration: " + e.getMessage());
        }
        Path config = ConfigFinder.find(base);
        return config != null ? config.getParent() : base;
    }

    /**
     * The targets to pass to axx running in a directory. Without a configured working directory,
     * the targets are relative to the project directory, so they are made relative to the one axx
     * runs in.
     */
    @NotNull List<String> targetsFor(@NotNull Path workingDirectory) throws ExecutionException {
        List<String> targets = getTargets();
        if (!getWorkingDirectory().isEmpty()) {
            return targets;
        }
        Path base = projectDirectory();
        List<String> resolved = new ArrayList<>();
        for (String target : targets) {
            try {
                Path path = base.resolve(RunTargets.pathOf(target));
                resolved.add(RunTargets.target(path, RunTargets.lineOf(target), workingDirectory));
            } catch (InvalidPathException e) {
                resolved.add(target);
            }
        }
        return resolved;
    }

    private @NotNull Path projectDirectory() throws ExecutionException {
        String basePath = getProject().getBasePath();
        if (basePath == null) {
            throw new ExecutionException("Cannot run axx: the project has no directory.");
        }
        return Path.of(basePath);
    }

    @Override
    public void checkConfiguration() throws RuntimeConfigurationException {
        try {
            Path dir = workingDirectory();
            if (!Files.isDirectory(dir)) {
                throw new RuntimeConfigurationError("The working directory does not exist: " + dir);
            }
        } catch (ExecutionException e) {
            throw new RuntimeConfigurationError(e.getMessage());
        }
    }

    @Override
    public @NotNull SettingsEditor<? extends RunConfiguration> getConfigurationEditor() {
        return new AxxRunConfigurationEditor(getProject());
    }

    @Override
    public @Nullable RunProfileState getState(
            @NotNull Executor executor, @NotNull ExecutionEnvironment environment) {
        return new AxxRunState(this, environment, null);
    }

    @Override
    public @NotNull SMTRunnerConsoleProperties createTestConsoleProperties(
            @NotNull Executor executor) {
        return new AxxTestConsoleProperties(this, executor);
    }
}
