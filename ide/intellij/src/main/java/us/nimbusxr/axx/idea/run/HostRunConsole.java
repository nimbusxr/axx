// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.ExecutionException;
import com.intellij.execution.Executor;
import com.intellij.execution.executors.DefaultRunExecutor;
import com.intellij.execution.process.ProcessHandler;
import com.intellij.execution.process.ProcessOutputTypes;
import com.intellij.execution.testframework.sm.SMTestRunnerConnectionUtil;
import com.intellij.execution.testframework.ui.BaseTestsOutputConsoleView;
import com.intellij.execution.ui.RunContentDescriptor;
import com.intellij.execution.ui.RunContentManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.Key;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.OutputStream;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * A run the axx host serves that the IDE did not start, such as an agent's in a sandbox, in the
 * test runner as it happens: a Run tool window tab whose console reads the run's TeamCity report as
 * the console of {@link AxxRunState} reads axx's output. Its process is not one: the host's events
 * bring the report's lines, what happens to the run, and its end.
 *
 * <p>Lines come as the host sends them, on any thread, before the tab shows too: it shows on the UI
 * thread, with every line that came before it.
 */
public final class HostRunConsole {
    private final Project project;
    private final String title;
    private final Path projectDir;
    private final HostRunHandler handler = new HostRunHandler();
    private volatile @Nullable RunContentDescriptor descriptor;

    /**
     * @param title the tab's title, such as {@code axx host: run 4 (axx run --compact)}
     * @param projectDir the project directory, where the run's features and files are
     */
    public HostRunConsole(
            @NotNull Project project, @NotNull String title, @NotNull Path projectDir) {
        this.project = project;
        this.title = title;
        this.projectDir = projectDir;
    }

    /**
     * Shows the tab in the Run tool window, without activating the tool window: the run is not the
     * person's. On the UI thread.
     *
     * @param reuse the tab of an earlier run to show it in, when that run ended; null for a new tab
     * @throws ExecutionException when the test runner's console cannot be made
     */
    public void show(@Nullable HostRunConsole reuse) throws ExecutionException {
        RunContentDescriptor old = reuse != null && reuse.isEnded() ? reuse.descriptor : null;
        RunContentDescriptor shown = attach();
        RunContentManager.getInstance(project)
                .showRunContent(DefaultRunExecutor.getRunExecutorInstance(), shown, old);
    }

    /**
     * Makes the tab's test runner console, which reads the run's lines from now on, those that
     * came before too. On the UI thread.
     */
    @NotNull RunContentDescriptor attach() throws ExecutionException {
        AxxRunConfiguration configuration =
                (AxxRunConfiguration)
                        AxxRunConfigurationType.getInstance()
                                .getFactory()
                                .createTemplateConfiguration(project);
        configuration.setName(title);
        configuration.setWorkingDirectory(projectDir.toString());
        Executor executor = DefaultRunExecutor.getRunExecutorInstance();
        AxxTestConsoleProperties properties =
                new AxxTestConsoleProperties(configuration, executor, projectDir);
        BaseTestsOutputConsoleView console =
                SMTestRunnerConnectionUtil.createAndAttachConsole(
                        AxxTestConsoleProperties.FRAMEWORK, handler, properties);
        RunContentDescriptor shown =
                new RunContentDescriptor(console, handler, console.getComponent(), title);
        shown.setActivateToolWindowWhenAdded(false);
        descriptor = shown;
        handler.startNotify();
        return shown;
    }

    /** The tab's content, once it shows. */
    public @Nullable RunContentDescriptor descriptor() {
        return descriptor;
    }

    /** Brings the tab to the front, and the Run tool window with it. On the UI thread. */
    public void toFront() {
        RunContentDescriptor shown = descriptor;
        if (shown != null) {
            RunContentManager.getInstance(project)
                    .toFrontRunContent(DefaultRunExecutor.getRunExecutorInstance(), shown);
        }
    }

    /** Prints what happens to the run, such as {@code run 4: ready in 2.1s}. */
    public void print(@NotNull String text) {
        handler.text(text.endsWith("\n") ? text : text + "\n", ProcessOutputTypes.SYSTEM);
    }

    /** Reads a line of the run's TeamCity report. */
    public void report(@NotNull String line) {
        handler.text(line + "\n", ProcessOutputTypes.STDOUT);
    }

    /** The run ended, as {@code axx run} with this exit code would. */
    public void end(int exitCode) {
        handler.end(exitCode);
    }

    /** Whether the run ended (or the person stopped watching it). */
    public boolean isEnded() {
        return handler.isEnded();
    }

    /**
     * The run's process, as the test runner sees it: what the host brings is its output, kept until
     * the console listens. Stop only stops showing the run, which goes on: the IDE does not stop
     * others' runs.
     */
    static final class HostRunHandler extends ProcessHandler {
        private final List<Runnable> pending = new ArrayList<>();
        private boolean started;
        private boolean ended;

        synchronized void text(@NotNull String text, @NotNull Key<?> type) {
            if (ended) {
                return;
            }
            if (started) {
                notifyTextAvailable(text, type);
            } else {
                pending.add(() -> notifyTextAvailable(text, type));
            }
        }

        synchronized void end(int exitCode) {
            if (ended) {
                return;
            }
            ended = true;
            if (started) {
                notifyProcessTerminated(exitCode);
            } else {
                pending.add(() -> notifyProcessTerminated(exitCode));
            }
        }

        synchronized boolean isEnded() {
            return ended;
        }

        @Override
        public synchronized void startNotify() {
            super.startNotify();
            started = true;
            for (Runnable notice : pending) {
                notice.run();
            }
            pending.clear();
        }

        @Override
        protected void destroyProcessImpl() {
            synchronized (this) {
                ended = true;
                pending.clear();
            }
            notifyProcessTerminated(-1);
        }

        @Override
        protected void detachProcessImpl() {
            synchronized (this) {
                ended = true;
                pending.clear();
            }
            notifyProcessDetached();
        }

        @Override
        public boolean detachIsDefault() {
            return true;
        }

        @Override
        public @Nullable OutputStream getProcessInput() {
            return null;
        }
    }
}
