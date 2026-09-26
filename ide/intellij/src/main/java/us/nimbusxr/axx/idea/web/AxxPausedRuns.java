// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.intellij.openapi.Disposable;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.components.Service;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.EditorFactory;
import com.intellij.openapi.editor.event.CaretEvent;
import com.intellij.openapi.editor.event.CaretListener;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.fileEditor.FileEditorManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.openapi.wm.StatusBar;
import com.intellij.util.Alarm;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicLong;

/**
 * The scenarios axx runs in the project have paused (see {@link ScenarioPause}): the paused run is
 * the one that paused last and has not resumed. While one is paused, the element of the step under
 * the caret in a feature file is highlighted on its page, with the answer in the status bar; and
 * the actions of {@link PausedScenarioActions} run a step in it and insert the steps recorded in
 * Playwright's Inspector.
 *
 * <p>Runs are told apart by an owner object, their test events converter. A run that paused
 * answers for the recorded steps until its process ends; the recording file of the last pause stays
 * known after that.
 */
@Service(Service.Level.PROJECT)
public final class AxxPausedRuns implements Disposable {
    private static final int HIGHLIGHT_DELAY_MS = 200;
    private static final String FEATURE_EXTENSION = "feature";
    private static final String STATUS = "axx: ";

    /**
     * A paused scenario.
     *
     * @param url where it answers, ending with {@code /}
     * @param location where it paused, such as {@code features/shop.feature:24}; null when unknown
     */
    public record Pause(@NotNull String url, @Nullable String location) {}

    private final Project project;
    private final PausedScenarioClient client = new PausedScenarioClient();
    // By run, in the order they paused: the paused scenarios, and the URLs of the runs that paused
    // and still run. The last entry is the one that counts.
    private final Map<Object, Pause> paused = new LinkedHashMap<>();
    private final Map<Object, String> live = new LinkedHashMap<>();
    private @Nullable Path recording;

    private final Alarm highlightAlarm;
    private final AtomicLong highlights = new AtomicLong();
    // On the UI thread: the URL and step last highlighted, and whether the status bar shows an
    // answer.
    private @Nullable String highlighted;
    private boolean showing;

    public AxxPausedRuns(@NotNull Project project) {
        this.project = project;
        this.highlightAlarm = new Alarm(Alarm.ThreadToUse.SWING_THREAD, this);
        EditorFactory.getInstance()
                .getEventMulticaster()
                .addCaretListener(
                        new CaretListener() {
                            @Override
                            public void caretPositionChanged(@NotNull CaretEvent event) {
                                Editor editor = event.getEditor();
                                if (editor.getProject() == project && current() != null) {
                                    scheduleHighlight(editor);
                                }
                            }
                        },
                        this);
    }

    public static @NotNull AxxPausedRuns getInstance(@NotNull Project project) {
        return project.getService(AxxPausedRuns.class);
    }

    /** The project's instance if it exists yet, which it does once a run of the project paused. */
    public static @Nullable AxxPausedRuns getIfCreated(@Nullable Project project) {
        return project == null || project.isDisposed()
                ? null
                : project.getServiceIfCreated(AxxPausedRuns.class);
    }

    /** A run's scenario paused. */
    public void paused(@NotNull Object run, @NotNull ScenarioPause pause) {
        synchronized (this) {
            paused.remove(run);
            paused.put(run, new Pause(pause.url(), pause.location()));
            live.remove(run);
            live.put(run, pause.url());
            if (pause.recording() != null) {
                recording = pause.recording();
            }
        }
        changed();
    }

    /** A run's scenario resumed. */
    public void resumed(@NotNull Object run, @NotNull String url) {
        synchronized (this) {
            Pause pause = paused.get(run);
            if (pause == null || !pause.url().equals(url)) {
                return;
            }
            paused.remove(run);
        }
        changed();
    }

    /** A run's process ended. */
    public void ended(@NotNull Object run) {
        synchronized (this) {
            boolean wasPaused = paused.remove(run) != null;
            if (live.remove(run) == null && !wasPaused) {
                return;
            }
        }
        changed();
    }

    /** The paused scenario, or null when none is. */
    public synchronized @Nullable Pause current() {
        Pause last = null;
        for (Pause pause : paused.values()) {
            last = pause;
        }
        return last;
    }

    /** The URL of the last run that paused, while it runs; else null. */
    public synchronized @Nullable String liveUrl() {
        String last = null;
        for (String url : live.values()) {
            last = url;
        }
        return last;
    }

    /** The recording file of the last pause, or null when no pause named one. */
    public synchronized @Nullable Path recording() {
        return recording;
    }

    @NotNull PausedScenarioClient client() {
        return client;
    }

    /** Shows the new state, and highlights the step under the caret of the selected editor. */
    private void changed() {
        ApplicationManager.getApplication()
                .invokeLater(
                        () -> {
                            highlighted = null;
                            highlights.incrementAndGet();
                            Pause pause = current();
                            if (pause != null) {
                                showPaused(pause);
                                Editor editor =
                                        FileEditorManager.getInstance(project)
                                                .getSelectedTextEditor();
                                if (editor != null) {
                                    scheduleHighlight(editor);
                                }
                            } else {
                                clearStatus();
                            }
                        },
                        project.getDisposed());
    }

    private void scheduleHighlight(Editor editor) {
        highlightAlarm.cancelAllRequests();
        highlightAlarm.addRequest(() -> highlight(editor), HIGHLIGHT_DELAY_MS);
    }

    /** Highlights the element of the step under an editor's caret. On the UI thread. */
    private void highlight(Editor editor) {
        Pause pause = current();
        if (pause == null || editor.isDisposed() || project.isDisposed()) {
            return;
        }
        Document document = editor.getDocument();
        VirtualFile file = FileDocumentManager.getInstance().getFile(document);
        if (file == null || !FEATURE_EXTENSION.equals(file.getExtension())) {
            return;
        }
        String step =
                FeatureSteps.stepAt(
                        FeatureSteps.lines(document.getImmutableCharSequence()),
                        editor.getCaretModel().getLogicalPosition().line);
        if (step == null) {
            if (highlighted != null) {
                highlighted = null;
                highlights.incrementAndGet();
                showPaused(pause);
            }
            return;
        }
        String asked = pause.url() + "\n" + step;
        if (asked.equals(highlighted)) {
            return;
        }
        highlighted = asked;
        long ask = highlights.incrementAndGet();
        client.highlight(pause.url(), step)
                .whenComplete(
                        (answer, error) -> {
                            // Errors are quiet: the scenario may have resumed in between.
                            if (answer == null
                                    || (answer.status() != 200 && answer.status() != 404)) {
                                return;
                            }
                            ApplicationManager.getApplication()
                                    .invokeLater(
                                            () -> {
                                                if (ask == highlights.get() && current() != null) {
                                                    showStatus(answer.text().strip());
                                                }
                                            },
                                            project.getDisposed());
                        });
    }

    private void showPaused(Pause pause) {
        showStatus(
                "scenario paused"
                        + (pause.location() != null ? " at " + pause.location() : "")
                        + ". Put the caret on a step to highlight its element.");
    }

    private void showStatus(String text) {
        StatusBar.Info.set(STATUS + text, project);
        showing = true;
    }

    private void clearStatus() {
        if (showing) {
            StatusBar.Info.set("", project);
            showing = false;
        }
    }

    @Override
    public void dispose() {}
}
