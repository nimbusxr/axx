// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.intellij.ide.BrowserUtil;
import com.intellij.ide.actions.RevealFileAction;
import com.intellij.notification.NotificationAction;
import com.intellij.notification.NotificationGroupManager;
import com.intellij.notification.NotificationType;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.diagnostic.Logger;
import com.intellij.openapi.project.Project;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.web.AxxBrowserTabs.Tab;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * Opens what scenarios keep: traces in Playwright's trace viewer, and videos. They open in editor
 * tabs (see {@link AxxBrowserTabs}); without the IDE's browser, traces open in the default browser
 * and videos in the default video player.
 */
public final class AxxWebViewer {
    private static final Logger LOG = Logger.getInstance(AxxWebViewer.class);

    // The trace viewer's files a run last announced: they stay on disk, for the traces of any run.
    private static volatile @Nullable Path lastViewerFolder;

    private AxxWebViewer() {}

    /** Remembers the folder of the trace viewer's files a run announced. */
    public static void rememberViewerFolder(@NotNull Path dir) {
        lastViewerFolder = dir;
    }

    /** Opens a trace or plays a video. Call on the UI thread. */
    public static void open(@NotNull Project project, @NotNull KeptFile file) {
        if (!Files.isRegularFile(file.path())) {
            notify(
                    project,
                    file.path() + " no longer exists. Run the scenario again.",
                    NotificationType.WARNING);
            return;
        }
        // Starting the file server takes a moment: not on the UI thread.
        ApplicationManager.getApplication()
                .executeOnPooledThread(
                        () -> {
                            String url =
                                    file.kind() == KeptFile.Kind.TRACE
                                            ? tracePage(file)
                                            : videoUrl(file);
                            ApplicationManager.getApplication()
                                    .invokeLater(
                                            () -> {
                                                if (!project.isDisposed()) {
                                                    show(project, file, url);
                                                }
                                            });
                        });
    }

    /**
     * The page that opens a trace in the trace viewer's files, served with the trace, when a run
     * announced their folder (they stay on disk, so this works after the run, offline); else null.
     */
    private static @Nullable String tracePage(KeptFile trace) {
        Path folder = TraceViewer.folder(trace.viewerFolder(), lastViewerFolder);
        if (folder == null) {
            return null;
        }
        try {
            AxxFileServer server = AxxFileServer.getInstance();
            return TraceViewer.page(server.folderUrl(folder), server.url(trace.path()));
        } catch (IOException e) {
            LOG.warn("axx: cannot serve " + trace.path(), e);
            return null;
        }
    }

    private static @Nullable String videoUrl(KeptFile video) {
        if (!AxxBrowserTabs.inIde()) {
            return null;
        }
        try {
            return AxxFileServer.getInstance().url(video.path());
        } catch (IOException e) {
            LOG.warn("axx: cannot serve " + video.path(), e);
            return null;
        }
    }

    private static void show(Project project, KeptFile file, @Nullable String url) {
        if (file.kind() == KeptFile.Kind.VIDEO) {
            if (url != null) {
                AxxBrowserTabs.show(project, Tab.VIDEO, "Video: " + file.label(), url);
            } else {
                BrowserUtil.open(file.path().toString());
            }
            return;
        }
        if (url != null) {
            AxxBrowserTabs.show(project, Tab.TRACE, "Trace: " + file.label(), url);
            return;
        }
        RevealFileAction.openFile(file.path());
        NotificationGroupManager.getInstance()
                .getNotificationGroup("axx")
                .createNotification(
                        "No Playwright trace viewer to open the trace in. Drop it on"
                                + " trace.playwright.dev to open it.",
                        NotificationType.INFORMATION)
                .addAction(
                        NotificationAction.createSimpleExpiring(
                                "Open trace.playwright.dev",
                                () -> BrowserUtil.browse(TraceViewer.SITE)))
                .notify(project);
    }

    private static void notify(Project project, String message, NotificationType type) {
        NotificationGroupManager.getInstance()
                .getNotificationGroup("axx")
                .createNotification(message, type)
                .notify(project);
    }
}
