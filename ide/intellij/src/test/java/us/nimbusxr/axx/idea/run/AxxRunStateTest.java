// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.Executor;
import com.intellij.execution.ExecutorRegistry;
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
import com.intellij.openapi.application.WriteAction;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VfsUtilCore;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.xdebugger.XDebuggerManager;
import com.intellij.xdebugger.XDebuggerUtil;
import com.intellij.xdebugger.breakpoints.XLineBreakpoint;

import org.jdom.Element;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.debugger.RemoteDebugConfiguration;

import us.nimbusxr.axx.idea.AxxSettings;
import us.nimbusxr.axx.idea.gherkin.AxxStepBreakpointType;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

/**
 * The axx command line a run configuration runs, with or without watching the browsers, the steps
 * it pauses before, and the Go debugger for step code.
 */
public class AxxRunStateTest extends HeavyPlatformTestCase {
    private static final String ORDERS =
            String.join(
                    "\n",
                    "Feature: Orders", // 1
                    "",
                    "  Background:", // 3
                    "    Given the orders service",
                    "", // 5
                    "  Scenario: List orders",
                    "    When a GET request is sent to \"/orders\"", // 7
                    "    Then the response status code is 200",
                    "");

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
            AxxSettings.getInstance()
                    .setWatchSlowdownMillis(AxxSettings.DEFAULT_WATCH_SLOWDOWN_MILLIS);
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

    public void testARelativeExecutableStartsAtTheProject() throws Exception {
        // The suite is in a folder of the project; the setting names axx from the project's
        // directory, as the language server reads it.
        Path local = Files.createDirectories(base.resolve("bin")).resolve("axx");
        Files.writeString(local, "");
        assertTrue(local.toFile().setExecutable(true));
        AxxSettings.getInstance().setExecutable("bin/axx");
        configuration.setTargets(List.of("features/orders.feature:7"));
        configuration.setWorkingDirectory(suite.toString());

        GeneralCommandLine command = commandLine(environment(false));
        assertEquals(local.toString(), command.getExePath());
        assertEquals(suite.toFile(), command.getWorkDirectory());
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

    public void testTellsAxxThatTheIdeRunsIt() throws Exception {
        assertEquals("intellij", commandLine(environment(false)).getEnvironment().get("AXX_IDE"));
    }

    public void testWatchRunsWatchTheBrowsers() throws Exception {
        configuration.setTargets(List.of("features/orders.feature:7"));
        configuration.setWorkingDirectory(suite.toString());
        Executor watch = ExecutorRegistry.getInstance().getExecutorById(AxxWatchExecutor.ID);
        assertNotNull("Watch is not registered", watch);
        assertInstanceOf(
                ProgramRunner.getRunner(AxxWatchExecutor.ID, configuration), AxxWatchRunner.class);

        ExecutionEnvironment environment =
                ExecutionEnvironmentBuilder.create(getProject(), watch, configuration).build();
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.watch=true",
                        "--set",
                        "packs.web-core.slowdown=300ms",
                        "features/orders.feature:7"),
                commandLine(environment).getParametersList().getList());
    }

    public void testTheConfigurationCanWatchEveryRun() throws Exception {
        configuration.setTargets(List.of("features/orders.feature:7"));
        configuration.setWorkingDirectory(suite.toString());
        configuration.setWatchBrowsers(true);
        AxxSettings.getInstance().setWatchSlowdownMillis(0);
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.watch=true",
                        "features/orders.feature:7"),
                commandLine(environment(false)).getParametersList().getList());
        // Debug watches too, and debugs.
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.watch=true",
                        "--set",
                        "packs.web-core.pauseOnFailure=true",
                        "features/orders.feature:7"),
                commandLine(environment(true)).getParametersList().getList());

        // The option is stored with the configuration.
        Element stored = new Element("configuration");
        configuration.writeExternal(stored);
        AxxRunConfiguration read =
                (AxxRunConfiguration)
                        AxxRunConfigurationType.getInstance()
                                .getFactory()
                                .createTemplateConfiguration(getProject());
        read.readExternal(stored);
        assertTrue(read.isWatchBrowsers());
        assertEquals(configuration.getTargets(), read.getTargets());
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
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.pauseOnFailure=true",
                        "features/orders.feature"),
                commandLine(environment).getParametersList().getList());
    }

    public void testDebugPausesAtStepBreakpointsAndFailures() throws Exception {
        configuration.setTargets(List.of("features/orders.feature:6"));
        configuration.setWorkingDirectory(suite.toString());
        Path orders = Files.writeString(suite.resolve("features/orders.feature"), ORDERS);
        Path returns = Files.writeString(suite.resolve("features/returns.feature"), ORDERS);
        addStepBreakpoint(orders, 8, true);
        addStepBreakpoint(orders, 4, true);
        addStepBreakpoint(orders, 7, false); // disabled
        addStepBreakpoint(returns, 7, true); // in a file the run does not reach

        // Breakpoints in step code stop too: --debug-steps stays.
        ExecutionEnvironment debug = environment(true);
        debug.putUserData(AxxDebugRunner.STEPS_PORT, 2345);
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--debug-steps=2345",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.pauseOnFailure=true",
                        "--pause-at",
                        "features/orders.feature:4",
                        "--pause-at",
                        "features/orders.feature:8",
                        "features/orders.feature:6"),
                commandLine(debug).getParametersList().getList());
        // Without a Go debugger, the run still pauses at the steps and failures.
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.pauseOnFailure=true",
                        "--pause-at",
                        "features/orders.feature:4",
                        "--pause-at",
                        "features/orders.feature:8",
                        "features/orders.feature:6"),
                commandLine(environment(true)).getParametersList().getList());
    }

    public void testRunAndWatchIgnoreStepBreakpoints() throws Exception {
        configuration.setTargets(List.of("features/orders.feature"));
        configuration.setWorkingDirectory(suite.toString());
        AxxSettings.getInstance().setWatchSlowdownMillis(0);
        Path orders = Files.writeString(suite.resolve("features/orders.feature"), ORDERS);
        addStepBreakpoint(orders, 8, true);

        // Watch only shows the browsers: no pausing at breakpoints or failures.
        Executor watch = ExecutorRegistry.getInstance().getExecutorById(AxxWatchExecutor.ID);
        ExecutionEnvironment environment =
                ExecutionEnvironmentBuilder.create(getProject(), watch, configuration).build();
        assertEquals(
                List.of(
                        "run",
                        "--format",
                        "teamcity",
                        "--workers",
                        "1",
                        "--set",
                        "packs.web-core.watch=true",
                        "features/orders.feature"),
                commandLine(environment).getParametersList().getList());
        // Run does not pause.
        assertEquals(
                List.of("run", "--format", "teamcity", "features/orders.feature"),
                commandLine(environment(false)).getParametersList().getList());
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

    /** Adds a step breakpoint on a one-based line, as clicking in the gutter does. */
    private void addStepBreakpoint(Path file, int line, boolean enabled) {
        assertNotNull(LocalFileSystem.getInstance().refreshAndFindFileByNioFile(file));
        AxxStepBreakpointType type =
                XDebuggerUtil.getInstance().findBreakpointType(AxxStepBreakpointType.class);
        assertNotNull("the step breakpoint type is not registered", type);
        WriteAction.run(
                () -> {
                    XLineBreakpoint<?> breakpoint =
                            XDebuggerManager.getInstance(getProject())
                                    .getBreakpointManager()
                                    .addLineBreakpoint(
                                            type,
                                            VfsUtilCore.pathToUrl(file.toString()),
                                            line - 1,
                                            null);
                    breakpoint.setEnabled(enabled);
                });
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
