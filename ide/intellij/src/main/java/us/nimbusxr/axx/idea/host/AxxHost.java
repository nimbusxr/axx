// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import com.intellij.diff.DiffContentFactory;
import com.intellij.diff.DiffManager;
import com.intellij.diff.contents.DocumentContent;
import com.intellij.diff.requests.SimpleDiffRequest;
import com.intellij.execution.ExecutionException;
import com.intellij.execution.configurations.GeneralCommandLine;
import com.intellij.notification.Notification;
import com.intellij.notification.NotificationAction;
import com.intellij.notification.NotificationGroupManager;
import com.intellij.notification.NotificationType;
import com.intellij.openapi.Disposable;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.application.ModalityState;
import com.intellij.openapi.components.Service;
import com.intellij.openapi.diagnostic.Logger;
import com.intellij.openapi.fileTypes.FileType;
import com.intellij.openapi.fileTypes.FileTypeManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.text.StringUtil;
import com.intellij.openapi.wm.impl.status.widget.StatusBarWidgetsManager;
import com.intellij.util.concurrency.AppExecutorUtil;
import com.intellij.util.messages.Topic;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxBinary;
import us.nimbusxr.axx.idea.ConfigFinder;
import us.nimbusxr.axx.idea.run.HostRunConsole;

import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.TimeUnit;

/**
 * The project's axx host, when the IDE started it: {@code axx host --ide}, in the folder of the
 * project's axx.yaml. Runs that cannot start the project's services themselves, such as an agent's
 * in a sandbox without Docker, ask it for them.
 *
 * <p>It asks the person the host's questions in notifications: whether the services may start by a
 * definition that changed since they approved one (asked for a run, or as the host starts). A
 * question stays until answered or replaced by a newer one. The answer is their click on the
 * notification (Approve or Refuse), which it writes to the host's standard input, which only the
 * IDE holds: there is no action that answers. It shows the runs the IDE did not start in the Run
 * tool window as they happen (the IDE's own show already), and the host's state in the status bar
 * (see {@link AxxHostWidgetFactory}). The host's log goes to idea.log.
 *
 * <p>It stops the host when the project closes: it closes the host's input, and the host stops the
 * services of a run in progress, cleans up after them and exits. It kills the host if it has not
 * exited a minute later.
 */
@Service(Service.Level.PROJECT)
public final class AxxHost implements Disposable {
    private static final Logger LOG = Logger.getInstance(AxxHost.class);

    /** The {@code AXX_IDE} of the runs this IDE starts, which show in the test runner already. */
    static final String OWN_IDE = "intellij";

    /** The notification group of the host's questions, which stay until answered or closed. */
    static final String QUESTIONS = "axx host";

    private static final long STOP_SECONDS = 60;

    /** Hears about the host's state, on the project's message bus. */
    public interface Listener {
        @Topic.ProjectLevel
        Topic<Listener> TOPIC = new Topic<>(Listener.class, Topic.BroadcastDirection.NONE);

        /** A host started, its state changed, or it stopped. */
        void changed();
    }

    private final Project project;
    private final Object lock = new Object();
    // Under lock: the last host started; null until one is.
    private @Nullable Session current;
    // On the UI thread: the tab of the last run shown.
    private @Nullable HostRunConsole lastShown;

    public AxxHost(@NotNull Project project) {
        this.project = project;
    }

    public static @NotNull AxxHost getInstance(@NotNull Project project) {
        return project.getService(AxxHost.class);
    }

    /** The project's instance if it exists yet, which it does once a host started. */
    public static @Nullable AxxHost getIfCreated(@Nullable Project project) {
        return project == null || project.isDisposed()
                ? null
                : project.getServiceIfCreated(AxxHost.class);
    }

    /**
     * Starts the host, unless one the IDE started runs, on a background thread.
     *
     * @param quiet true when the project opens: when its axx.yaml declares no services, there is
     *     then no host without a word
     */
    public void start(boolean quiet) {
        ApplicationManager.getApplication().executeOnPooledThread(() -> startNow(quiet));
    }

    private synchronized void startNow(boolean quiet) {
        if (isRunning() || project.isDisposed()) {
            return;
        }
        String base = project.getBasePath();
        Path config = base == null ? null : ConfigFinder.find(Path.of(base));
        if (config == null) {
            if (!quiet) {
                notify(
                        "Cannot start the axx host: the project has no axx.yaml.",
                        NotificationType.WARNING);
            }
            return;
        }
        Path dir = config.getParent();
        Process process;
        try {
            Path axx = AxxBinary.find(Path.of(base), "start the axx host");
            process =
                    new GeneralCommandLine(axx.toString(), "host", "--ide")
                            .withWorkingDirectory(dir)
                            .withCharset(StandardCharsets.UTF_8)
                            .createProcess();
        } catch (ExecutionException e) {
            notify(e.getMessage(), NotificationType.ERROR);
            return;
        }
        LOG.info("axx host: started in " + dir);
        Session session = new Session(dir, quiet);
        session.process = new HostProcess(process, session);
        synchronized (lock) {
            current = session;
        }
        session.process.start(
                task -> ApplicationManager.getApplication().executeOnPooledThread(task));
        changed();
        updateWidget();
    }

    /** Stops the host, which stops the services of a run in progress and cleans up first. */
    public void stop() {
        Session session;
        synchronized (lock) {
            session = current;
            if (session == null || session.exit != null || session.stopping) {
                return;
            }
            session.stopping = true;
        }
        HostProcess process = session.process;
        process.stop();
        AppExecutorUtil.getAppScheduledExecutorService()
                .schedule(
                        () -> {
                            if (process.isAlive()) {
                                LOG.warn(
                                        "axx host: still running a minute after it was asked to"
                                                + " stop; killing it");
                                process.kill();
                            }
                        },
                        STOP_SECONDS,
                        TimeUnit.SECONDS);
    }

    @Override
    public void dispose() {
        stop();
    }

    /** Whether a host the IDE started runs: it started, and has not ended. */
    public boolean isRunning() {
        synchronized (lock) {
            return current != null && current.exit == null;
        }
    }

    /** Whether the status bar shows the host: one started, and axx.yaml declares services. */
    boolean isShown() {
        synchronized (lock) {
            return current != null && current.exit != HostState.Exit.NO_SERVICES;
        }
    }

    /** The status bar's text. */
    @NotNull String text() {
        synchronized (lock) {
            return current == null ? "axx host" : current.state.text();
        }
    }

    /** The status bar's tooltip, as lines. */
    @NotNull List<String> tooltip() {
        synchronized (lock) {
            return current == null ? List.of() : current.state.tooltip();
        }
    }

    /** The tab of the last run shown; for tests. On the UI thread. */
    @Nullable HostRunConsole lastShown() {
        return lastShown;
    }

    /** The host's state; for tests. */
    @Nullable HostState state() {
        synchronized (lock) {
            return current == null ? null : current.state;
        }
    }

    /** Whether a question of the host waits for the person's answer. */
    public boolean hasPendingApproval() {
        synchronized (lock) {
            return current != null && current.state.pendingApproval() != null;
        }
    }

    /**
     * Shows the host's first question that waits for an answer again. It does not answer it: only
     * the person does, with a click on it. On the UI thread.
     */
    public void showApproval() {
        Session session;
        HostEvent question;
        synchronized (lock) {
            session = current;
            question = session == null ? null : session.state.pendingApproval();
        }
        if (question != null) {
            session.ask(question.id());
        }
    }

    /**
     * What a click on the status bar shows: the question that waits for an answer, else the run the
     * services are for. On the UI thread.
     */
    void showCurrent() {
        HostRunConsole console;
        synchronized (lock) {
            if (current == null || current.state.pendingApproval() != null) {
                console = null;
            } else {
                HostEvent run = current.state.currentRun();
                console = run == null ? null : current.shown.get(run.run());
            }
        }
        if (console != null) {
            console.toFront();
        } else {
            showApproval();
        }
    }

    /** A question's title: the run that asked, if one did. */
    static @NotNull String questionTitle(@NotNull HostEvent question) {
        return question.run() == 0
                ? "axx host: approve the services?"
                : "axx host: approve run " + question.run() + "?";
    }

    /**
     * A question's text: what changed and the run that needs it, if one asked (else the host asked
     * as it started), and what in the change reaches past a container.
     */
    static @NotNull String questionText(@NotNull HostEvent question) {
        StringBuilder html = new StringBuilder();
        if (question.run() == 0) {
            html.append("The services' definition changed since you last approved it. Approve?");
        } else {
            html.append("Run ").append(question.run());
            if (!question.command().isEmpty()) {
                html.append(" (")
                        .append(StringUtil.escapeXmlEntities(question.command()))
                        .append(")");
            }
            html.append(" needs services whose definition changed since you approved it. Approve?");
        }
        appendWarnings(html, question.warnings());
        return html.toString();
    }

    private static void appendWarnings(StringBuilder html, List<String> warnings) {
        if (warnings.isEmpty()) {
            return;
        }
        html.append("<ul>");
        for (String warning : warnings) {
            html.append("<li>").append(StringUtil.escapeXmlEntities(warning)).append("</li>");
        }
        html.append("</ul>");
    }

    /** Shows what a question asks to approve: the approved definition and the proposed one. */
    private void showDiff(HostEvent question) {
        DiffContentFactory contents = DiffContentFactory.getInstance();
        FileType yaml = FileTypeManager.getInstance().getFileTypeByExtension("yaml");
        DocumentContent approved = contents.create(project, question.approved(), yaml);
        DocumentContent proposed = contents.create(project, question.proposed(), yaml);
        approved.getDocument().setReadOnly(true);
        proposed.getDocument().setReadOnly(true);
        DiffManager.getInstance()
                .showDiff(
                        project,
                        new SimpleDiffRequest(
                                question.run() == 0
                                        ? "axx host: the services"
                                        : "axx host: run " + question.run() + "'s services",
                                approved,
                                proposed,
                                question.approved().isEmpty() ? "Approved (none)" : "Approved",
                                "Proposed"));
    }

    private void changed() {
        if (!project.isDisposed()) {
            project.getMessageBus().syncPublisher(Listener.TOPIC).changed();
        }
    }

    /** Shows or hides the status bar's item, which shows once a host started. */
    private void updateWidget() {
        onUiThread(
                () ->
                        project.getService(StatusBarWidgetsManager.class)
                                .updateWidget(AxxHostWidgetFactory.class));
    }

    private void onUiThread(Runnable task) {
        ApplicationManager.getApplication()
                .invokeLater(task, ModalityState.any(), project.getDisposed());
    }

    private void notify(String content, NotificationType type) {
        if (project.isDisposed()) {
            return;
        }
        NotificationGroupManager.getInstance()
                .getNotificationGroup("axx")
                .createNotification(
                        StringUtil.escapeXmlEntities(content).replace("\n", "<br>"), type)
                .notify(project);
    }

    /** A host the IDE started: its process and state, its questions, and the runs it shows. */
    private final class Session implements HostProcess.Listener {
        private final Path dir;
        private final boolean quiet;
        private final HostState state = new HostState();
        // The runs shown in the Run tool window until they end, and their project directories as
        // they see them, by number.
        private final Map<Integer, HostRunConsole> shown = new ConcurrentHashMap<>();
        private final Map<Integer, String> runDirs = new ConcurrentHashMap<>();
        // On the UI thread: the notifications that ask its questions, by id.
        private final Map<Integer, Notification> questions = new HashMap<>();
        private HostProcess process;
        // Under lock: the IDE asked it to stop; how it ended, once it did.
        private boolean stopping;
        private @Nullable HostState.Exit exit;

        Session(Path dir, boolean quiet) {
            this.dir = dir;
            this.quiet = quiet;
        }

        @Override
        public void event(@NotNull HostEvent event) {
            synchronized (lock) {
                state.apply(event);
            }
            switch (event.event()) {
                case HostEvent.SERVING -> serving(event);
                case HostEvent.RUN -> run(event);
                case HostEvent.REPORT -> {
                    report(event);
                    return;
                }
                case HostEvent.APPROVAL -> onUiThread(() -> ask(event.id()));
                case HostEvent.ANSWERED -> onUiThread(() -> forget(event.id()));
                default -> {}
            }
            changed();
        }

        private void serving(HostEvent event) {
            LOG.info("axx host: serving " + event.dir() + " on " + event.addr());
            if (event.warnings().isEmpty()) {
                return;
            }
            StringBuilder html =
                    new StringBuilder("What the services reach past their containers:");
            appendWarnings(html, event.warnings());
            NotificationGroupManager.getInstance()
                    .getNotificationGroup("axx")
                    .createNotification("axx host", html.toString(), NotificationType.WARNING)
                    .notify(project);
        }

        /** Shows a run the IDE did not start as it happens, until it ends. */
        private void run(HostEvent event) {
            if (OWN_IDE.equals(event.ide())) {
                return;
            }
            HostRunConsole console = shown.get(event.run());
            if (console == null) {
                if (HostEvent.ENDED.equals(event.state())) {
                    return;
                }
                console =
                        new HostRunConsole(
                                project,
                                "axx host: run " + event.run() + " (" + event.command() + ")",
                                projectDir());
                shown.put(event.run(), console);
                HostRunConsole created = console;
                ApplicationManager.getApplication()
                        .invokeLater(
                                () -> {
                                    try {
                                        created.show(lastShown);
                                        lastShown = created;
                                    } catch (ExecutionException e) {
                                        LOG.warn("axx host: cannot show run " + event.run(), e);
                                        created.end(-1);
                                    }
                                },
                                ModalityState.nonModal(),
                                project.getDisposed());
            }
            if (!event.dir().isEmpty()) {
                runDirs.put(event.run(), event.dir());
            }
            console.print(HostState.describe(event));
            if (HostEvent.ENDED.equals(event.state())) {
                console.end(HostState.exitCode(event));
                shown.remove(event.run());
                runDirs.remove(event.run());
            }
        }

        private void report(HostEvent event) {
            HostRunConsole console = shown.get(event.run());
            if (console != null) {
                console.report(
                        ReportLocations.relocate(
                                event.line(),
                                runDirs.get(event.run()),
                                projectDir().toString()));
            }
        }

        /** The project directory, as the host sees it. */
        private Path projectDir() {
            HostEvent serving;
            synchronized (lock) {
                serving = state.serving();
            }
            return serving == null || serving.dir().isEmpty() ? dir : Path.of(serving.dir());
        }

        /** Asks the person a question, in a notification. On the UI thread. */
        void ask(int id) {
            HostEvent question;
            synchronized (lock) {
                question = state.approval(id);
            }
            forget(id);
            if (question == null) {
                return;
            }
            Notification notification =
                    NotificationGroupManager.getInstance()
                            .getNotificationGroup(QUESTIONS)
                            .createNotification(
                                    questionTitle(question),
                                    questionText(question),
                                    NotificationType.WARNING);
            // Show Diff keeps the question open: it asks it again, next to the diff.
            notification.addAction(
                    NotificationAction.createSimple(
                            "Show Diff",
                            () -> {
                                showDiff(question);
                                ask(id);
                            }));
            notification.addAction(
                    NotificationAction.createSimpleExpiring("Approve", () -> answer(id, true)));
            notification.addAction(
                    NotificationAction.createSimpleExpiring("Refuse", () -> answer(id, false)));
            questions.put(id, notification);
            notification.notify(project);
        }

        /** Closes the notification of a question. On the UI thread. */
        private void forget(int id) {
            Notification notification = questions.remove(id);
            if (notification != null) {
                notification.expire();
            }
        }

        /** Writes the person's answer, their click on the question, to the host's input. */
        private void answer(int id, boolean approve) {
            synchronized (lock) {
                if (state.approval(id) == null) {
                    return;
                }
            }
            if (!process.answer(id, approve)) {
                AxxHost.this.notify(
                        "Cannot answer the axx host: it stopped.", NotificationType.ERROR);
            }
        }

        @Override
        public void log(@NotNull String line) {
            LOG.info("axx host: " + line);
        }

        @Override
        public void exited(int exitCode, @NotNull String errors) {
            HostState.Exit how;
            boolean asked;
            synchronized (lock) {
                how = state.exited(exitCode, errors);
                exit = how;
                asked = stopping;
            }
            LOG.info("axx host: exited with " + exitCode);
            for (HostRunConsole console : shown.values()) {
                console.print("The axx host stopped.");
                console.end(-1);
            }
            shown.clear();
            onUiThread(
                    () -> {
                        for (Integer id : new ArrayList<>(questions.keySet())) {
                            forget(id);
                        }
                    });
            switch (how) {
                case NO_SERVICES -> {
                    if (!quiet) {
                        AxxHost.this.notify(
                                HostState.errorMessage(errors), NotificationType.INFORMATION);
                    }
                }
                case FAILED -> {
                    if (!asked) {
                        AxxHost.this.notify(
                                "The axx host did not start:\n" + errors, NotificationType.ERROR);
                    }
                }
                case STOPPED -> {
                    if (!asked && exitCode != 0) {
                        AxxHost.this.notify(
                                "The axx host stopped:\n" + errors, NotificationType.ERROR);
                    }
                }
                case ELSEWHERE -> {
                    // The status bar says so: the person started one in a terminal.
                }
            }
            changed();
            updateWidget();
        }
    }
}
