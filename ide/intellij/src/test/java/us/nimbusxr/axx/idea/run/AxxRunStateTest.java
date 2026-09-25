// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.RunManager;
import com.intellij.execution.RunnerAndConfigurationSettings;
import com.intellij.execution.configurations.ConfigurationFactory;
import com.intellij.execution.configurations.ConfigurationType;
import com.intellij.execution.configurations.ConfigurationTypeBase;
import com.intellij.execution.configurations.GeneralCommandLine;
import com.intellij.execution.configurations.RunConfiguration;
import com.intellij.execution.executors.DefaultDebugExecutor;
import com.intellij.execution.executors.DefaultRunExecutor;
import com.intellij.execution.runners.ExecutionEnvironment;
import com.intellij.execution.runners.ExecutionEnvironmentBuilder;
import com.intellij.execution.runners.ProgramRunner;
import com.intellij.icons.AllIcons;
import com.intellij.openapi.project.Project;
import com.intellij.testFramework.HeavyPlatformTestCase;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.debugger.RemoteDebugConfiguration;

import us.nimbusxr.axx.idea.AxxSettings;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

/** The axx command line a run configuration runs, and the Go debugger for step code. */
public class AxxRunStateTest extends HeavyPlatformTestCase {
    private Path base;
    private Path suite;
    private Path axx;
    private AxxRunConfiguration configuration;

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        base = Path.of(getProject().getBasePath());
        suite = Files.createDirectories(base.resolve("acceptance/features")).getParent();
        Files.writeString(suite.resolve("axx.yaml"), "version: 1\n");
        Files.writeString(suite.resolve("features/orders.feature"), "Feature: Orders\n");
        axx = Files.writeString(base.resolve("axx-bin"), "");
        assertTrue(axx.toFile().setExecutable(true));
        AxxSettings.getInstance().setExecutable(axx.toString());
        configuration =
                (AxxRunConfiguration)
                        AxxRunConfigurationType.getInstance()
                                .getFactory()
                                .createTemplateConfiguration(getProject());
    }

    @Override
    protected void tearDown() throws Exception {
        try {
            AxxSettings.getInstance().setExecutable(null);
        } finally {
            super.tearDown();
        }
    }

    public void testRunsTargetsInTheConfiguredDirectory() throws Exception {
        configuration.setTargets(List.of("features/orders.feature:7"));
        configuration.setArguments("--tags \"@smoke and not @wip\"");
        configuration.setWorkingDirectory(suite.toString());

        GeneralCommandLine command = commandLine(environment(false));
        assertEquals(axx.toString(), command.getExePath());
        assertEquals(suite.toFile(), command.getWorkDirectory());
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "features/orders.feature:7",
                        "--tags",
                        "@smoke and not @wip"),
                command.getParametersList().getList());
    }

    public void testFindsTheSuiteAboveTheFirstTarget() throws Exception {
        configuration.setTargets(List.of("acceptance/features/orders.feature:7"));

        GeneralCommandLine command = commandLine(environment(false));
        assertEquals(suite.toFile(), command.getWorkDirectory());
        assertEquals(
                List.of("run", "--format", "teamcity", "features/orders.feature:7"),
                command.getParametersList().getList());
    }

    public void testRunsTheWholeSuiteWithoutTargets() throws Exception {
        GeneralCommandLine command = commandLine(environment(false));
        assertEquals(suite.toFile(), command.getWorkDirectory());
        assertEquals(List.of("run", "--format", "teamcity"), command.getParametersList().getList());
    }

    public void testDebugsStepsWithAGoDebugger() throws Exception {
        configuration.setTargets(List.of("features/orders.feature"));
        configuration.setWorkingDirectory(suite.toString());
        ExecutionEnvironment environment = environment(true);
        environment.putUserData(AxxDebugRunner.STEPS_PORT, 2345);
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--debug-steps=2345",
                        "features/orders.feature"),
                commandLine(environment).getParametersList().getList());
    }

    public void testDebugRunsWithTheAxxRunner() {
        assertInstanceOf(
                ProgramRunner.getRunner(DefaultDebugExecutor.EXECUTOR_ID, configuration),
                AxxDebugRunner.class);
        assertFalse(
                ProgramRunner.getRunner(DefaultRunExecutor.EXECUTOR_ID, configuration)
                        instanceof AxxDebugRunner);
    }

    public void testNoStepsDebuggerWithoutTheGoPlugin() {
        assertNull(AxxStepsDebugger.prepare(getProject()));
        assertNull(
                RunManager.getInstance(getProject())
                        .findConfigurationByName(AxxStepsDebugger.CONFIGURATION));
    }

    public void testCreatesTheStepsDebuggerWithTheGoPlugin() {
        FakeGoRemoteType type = new FakeGoRemoteType();
        ConfigurationType.CONFIGURATION_TYPE_EP
                .getPoint()
                .registerExtension(type, getTestRootDisposable());

        assertEquals(Integer.valueOf(2345), AxxStepsDebugger.prepare(getProject()));
        RunnerAndConfigurationSettings settings =
                RunManager.getInstance(getProject())
                        .findConfigurationByName(AxxStepsDebugger.CONFIGURATION);
        assertNotNull(settings);
        FakeGoRemote remote = (FakeGoRemote) settings.getConfiguration();
        // Remote debug configurations keep the loopback address (127.0.0.1) as no host.
        assertNull(remote.getHost());
        assertEquals(2345, remote.getPort());

        // An existing configuration is kept, and its port used.
        remote.setPort(4567);
        assertEquals(Integer.valueOf(4567), AxxStepsDebugger.prepare(getProject()));
        RunManager.getInstance(getProject()).removeConfiguration(settings);
    }

    private GeneralCommandLine commandLine(ExecutionEnvironment environment) throws Exception {
        return new AxxRunState(configuration, environment, null).commandLine();
    }

    private ExecutionEnvironment environment(boolean debug) throws Exception {
        return ExecutionEnvironmentBuilder.create(
                        getProject(),
                        debug
                                ? DefaultDebugExecutor.getDebugExecutorInstance()
                                : DefaultRunExecutor.getRunExecutorInstance(),
                        configuration)
                .build();
    }

    /** Stands in for the Go plugin's Go Remote type: a platform remote debug configuration. */
    private static final class FakeGoRemoteType extends ConfigurationTypeBase {
        FakeGoRemoteType() {
            super(
                    AxxStepsDebugger.GO_REMOTE_TYPE,
                    "Go Remote",
                    null,
                    AllIcons.RunConfigurations.Remote);
            addFactory(
                    new ConfigurationFactory(this) {
                        @Override
                        public @NotNull String getId() {
                            return "Go Remote";
                        }

                        @Override
                        public @NotNull RunConfiguration createTemplateConfiguration(
                                @NotNull Project project) {
                            return new FakeGoRemote(project, this);
                        }
                    });
        }
    }

    private static final class FakeGoRemote extends RemoteDebugConfiguration {
        FakeGoRemote(Project project, ConfigurationFactory factory) {
            super(project, factory, "", 2345);
        }

        @Override
        public @NotNull com.intellij.xdebugger.XDebugProcess createDebugProcess(
                @NotNull java.net.InetSocketAddress socketAddress,
                @NotNull com.intellij.xdebugger.XDebugSession session,
                com.intellij.execution.ExecutionResult executionResult,
                @NotNull ExecutionEnvironment environment) {
            throw new UnsupportedOperationException();
        }
    }
}
