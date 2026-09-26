// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.intellij.notification.NotificationGroupManager;
import com.intellij.notification.NotificationType;
import com.intellij.openapi.actionSystem.ActionUpdateThread;
import com.intellij.openapi.actionSystem.AnActionEvent;
import com.intellij.openapi.actionSystem.CommonDataKeys;
import com.intellij.openapi.command.WriteCommandAction;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.EditorModificationUtil;
import com.intellij.openapi.editor.ScrollType;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.progress.ProgressIndicator;
import com.intellij.openapi.progress.ProgressManager;
import com.intellij.openapi.progress.Task;
import com.intellij.openapi.project.DumbAwareAction;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.text.StringUtil;
import com.intellij.openapi.vfs.VirtualFile;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * The editor actions of feature files for a paused scenario (see {@link AxxPausedRuns}): <em>Run
 * Step in Paused Scenario</em> and <em>Insert Recorded Steps</em>.
 */
public abstract class PausedScenarioActions extends DumbAwareAction {
    private static final String FEATURE_EXTENSION = "feature";

    @Override
    public @NotNull ActionUpdateThread getActionUpdateThread() {
        return ActionUpdateThread.BGT;
    }

    /** The editor of a feature file the action is for, or null. */
    static @Nullable Editor featureEditor(@NotNull AnActionEvent e) {
        Editor editor = e.getData(CommonDataKeys.EDITOR);
        VirtualFile file =
                editor == null ? null : FileDocumentManager.getInstance().getFile(editor.getDocument());
        return file != null && FEATURE_EXTENSION.equals(file.getExtension()) ? editor : null;
    }

    static int caretLine(@NotNull Editor editor) {
        return editor.getCaretModel().getLogicalPosition().line;
    }

    static void notify(
            @NotNull Project project,
            @NotNull String title,
            @NotNull String content,
            @NotNull NotificationType type) {
        NotificationGroupManager.getInstance()
                .getNotificationGroup("axx")
                .createNotification(title, html(content), type)
                .notify(project);
    }

    /** Text as notification content: escaped, with its line breaks. */
    private static String html(String text) {
        return StringUtil.escapeXmlEntities(text.strip()).replace("\n", "<br>");
    }

    /**
     * Runs the step under the caret, with the rows of its table, in the paused scenario, and tells
     * how it went in a notification.
     */
    public static final class RunStep extends PausedScenarioActions {
        @Override
        public void update(@NotNull AnActionEvent e) {
            Editor editor = featureEditor(e);
            AxxPausedRuns runs = AxxPausedRuns.getIfCreated(e.getProject());
            boolean paused = editor != null && runs != null && runs.current() != null;
            e.getPresentation().setVisible(paused);
            e.getPresentation()
                    .setEnabled(
                            paused
                                    && FeatureSteps.stepAt(
                                                    FeatureSteps.lines(
                                                            editor.getDocument()
                                                                    .getImmutableCharSequence()),
                                                    caretLine(editor))
                                            != null);
        }

        @Override
        public void actionPerformed(@NotNull AnActionEvent e) {
            Project project = e.getProject();
            Editor editor = featureEditor(e);
            AxxPausedRuns runs = AxxPausedRuns.getIfCreated(project);
            AxxPausedRuns.Pause pause = runs == null ? null : runs.current();
            if (project == null || editor == null || pause == null) {
                return;
            }
            String step =
                    FeatureSteps.stepWithTable(
                            FeatureSteps.lines(editor.getDocument().getImmutableCharSequence()),
                            caretLine(editor));
            if (step == null) {
                return;
            }
            String name = step.lines().findFirst().orElse(step);
            PausedScenarioClient client = runs.client();
            new Task.Backgroundable(project, "Running step: " + name, true) {
                @Override
                public void run(@NotNull ProgressIndicator indicator) {
                    indicator.setIndeterminate(true);
                    PausedScenarioClient.Answer answer;
                    try {
                        answer = await(client.run(pause.url(), step), indicator);
                    } catch (IOException ex) {
                        PausedScenarioActions.notify(
                                project,
                                "Couldn't run the step",
                                "The paused scenario did not answer: " + ex.getMessage(),
                                NotificationType.ERROR);
                        return;
                    }
                    switch (answer.status()) {
                        case 200 -> PausedScenarioActions.notify(
                                project, "", "Passed: " + name, NotificationType.INFORMATION);
                        case 422 -> PausedScenarioActions.notify(
                                project, "Failed: " + name, answer.text(), NotificationType.ERROR);
                        case 409 -> PausedScenarioActions.notify(
                                project,
                                "",
                                "No scenario is paused any more.",
                                NotificationType.WARNING);
                        default -> PausedScenarioActions.notify(
                                project,
                                "Couldn't run the step",
                                "The paused scenario answered " + answer.status() + ": " + answer.text(),
                                NotificationType.ERROR);
                    }
                }
            }.queue();
        }

        /** Waits for an answer, as long as it takes, unless the task is cancelled. */
        private static PausedScenarioClient.Answer await(
                CompletableFuture<PausedScenarioClient.Answer> answer, ProgressIndicator indicator)
                throws IOException {
            try {
                while (true) {
                    try {
                        indicator.checkCanceled();
                    } catch (RuntimeException cancelled) {
                        answer.cancel(true);
                        throw cancelled;
                    }
                    try {
                        return answer.get(100, TimeUnit.MILLISECONDS);
                    } catch (TimeoutException ignored) {
                        // not yet
                    }
                }
            } catch (ExecutionException ex) {
                Throwable cause = ex.getCause() != null ? ex.getCause() : ex;
                throw new IOException(
                        cause.getMessage() != null ? cause.getMessage() : cause.toString(), cause);
            } catch (InterruptedException ex) {
                Thread.currentThread().interrupt();
                answer.cancel(true);
                throw new IOException("interrupted", ex);
            }
        }
    }

    /**
     * Inserts the steps recorded in Playwright's Inspector below the caret's line, indented as the
     * step there (else as recorded): from the run that paused while it runs, else from the
     * recording file of the last pause.
     */
    public static final class InsertRecordedSteps extends PausedScenarioActions {
        @Override
        public void update(@NotNull AnActionEvent e) {
            Editor editor = featureEditor(e);
            AxxPausedRuns runs = AxxPausedRuns.getIfCreated(e.getProject());
            boolean available = editor != null && runs != null && hasSource(runs);
            e.getPresentation().setEnabledAndVisible(available);
        }

        private static boolean hasSource(AxxPausedRuns runs) {
            Path recording = runs.recording();
            return runs.liveUrl() != null || (recording != null && Files.isRegularFile(recording));
        }

        @Override
        public void actionPerformed(@NotNull AnActionEvent e) {
            Project project = e.getProject();
            Editor editor = featureEditor(e);
            AxxPausedRuns runs = AxxPausedRuns.getIfCreated(project);
            if (project == null || editor == null || runs == null) {
                return;
            }
            String liveUrl = runs.liveUrl();
            Path recording = runs.recording();
            PausedScenarioClient client = runs.client();
            String recorded;
            try {
                recorded =
                        ProgressManager.getInstance()
                                .runProcessWithProgressSynchronously(
                                        () -> client.recordedSteps(liveUrl, recording),
                                        "Getting Recorded Steps",
                                        true,
                                        project);
            } catch (IOException ex) {
                notify(
                        project,
                        "Couldn't get the recorded steps",
                        String.valueOf(ex.getMessage()),
                        NotificationType.ERROR);
                return;
            }
            if (recorded == null || recorded.isBlank()) {
                notify(
                        project,
                        "",
                        "No steps recorded yet. Record them in Playwright's Inspector while the"
                                + " scenario is paused.",
                        NotificationType.INFORMATION);
                return;
            }
            insert(project, editor, recorded);
        }

        private static void insert(Project project, Editor editor, String recorded) {
            Document document = editor.getDocument();
            if (!EditorModificationUtil.checkModificationAllowed(editor)
                    || !FileDocumentManager.getInstance().requestWriting(document, project)) {
                return;
            }
            int line = caretLine(editor);
            List<String> lines = FeatureSteps.lines(document.getImmutableCharSequence());
            String indent =
                    FeatureSteps.stepAt(lines, line) != null
                            ? FeatureSteps.indentOf(lines.get(line))
                            : null;
            String steps = FeatureSteps.reindent(recorded, indent);
            WriteCommandAction.runWriteCommandAction(
                    project,
                    "Insert Recorded Steps",
                    null,
                    () -> {
                        int count = document.getLineCount();
                        int at =
                                count == 0
                                        ? 0
                                        : document.getLineEndOffset(Math.min(line, count - 1));
                        String text = document.getTextLength() == 0 ? steps : "\n" + steps;
                        document.insertString(at, text);
                        editor.getCaretModel().moveToOffset(at + text.length());
                        editor.getScrollingModel().scrollToCaret(ScrollType.MAKE_VISIBLE);
                    });
        }
    }
}
