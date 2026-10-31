// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.text.StringUtil;
import com.intellij.openapi.wm.StatusBar;
import com.intellij.openapi.wm.StatusBarWidget;
import com.intellij.openapi.wm.StatusBarWidgetFactory;
import com.intellij.util.Consumer;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.awt.Component;
import java.awt.event.MouseEvent;
import java.util.List;

/**
 * The axx host's item in the status bar, once the IDE started one in the project: {@code axx host:}
 * and its state (ready; run 4 and its command, starting, running or cleaning up; waiting for your
 * approval; stopped; already running in a terminal), with where it listens, its services and their
 * sources on hover. A click shows the question that waits for an answer, else the run's tab.
 */
public final class AxxHostWidgetFactory implements StatusBarWidgetFactory {
    /** The widget's ID, as plugin.xml declares the factory. */
    static final String ID = "us.nimbusxr.axx.idea.host";

    @Override
    public @NotNull String getId() {
        return ID;
    }

    @Override
    public @NotNull String getDisplayName() {
        return "axx host";
    }

    @Override
    public boolean isAvailable(@NotNull Project project) {
        AxxHost host = AxxHost.getIfCreated(project);
        return host != null && host.isShown();
    }

    @Override
    public @NotNull StatusBarWidget createWidget(@NotNull Project project) {
        return new Widget(project);
    }

    private static final class Widget implements StatusBarWidget, StatusBarWidget.TextPresentation {
        private final Project project;
        private @Nullable StatusBar statusBar;

        Widget(Project project) {
            this.project = project;
            project.getMessageBus()
                    .connect(this)
                    .subscribe(
                            AxxHost.Listener.TOPIC,
                            () ->
                                    ApplicationManager.getApplication()
                                            .invokeLater(
                                                    () -> {
                                                        if (statusBar != null) {
                                                            statusBar.updateWidget(ID);
                                                        }
                                                    },
                                                    project.getDisposed()));
        }

        @Override
        public @NotNull String ID() {
            return ID;
        }

        @Override
        public @NotNull WidgetPresentation getPresentation() {
            return this;
        }

        @Override
        public void install(@NotNull StatusBar statusBar) {
            this.statusBar = statusBar;
        }

        @Override
        public @NotNull String getText() {
            return AxxHost.getInstance(project).text();
        }

        @Override
        public float getAlignment() {
            return Component.CENTER_ALIGNMENT;
        }

        @Override
        public @Nullable String getTooltipText() {
            List<String> lines = AxxHost.getInstance(project).tooltip();
            if (lines.isEmpty()) {
                return null;
            }
            StringBuilder html = new StringBuilder("<html>");
            for (int i = 0; i < lines.size(); i++) {
                html.append(i == 0 ? "" : "<br>")
                        .append(StringUtil.escapeXmlEntities(lines.get(i)));
            }
            return html.append("</html>").toString();
        }

        @Override
        public @Nullable Consumer<MouseEvent> getClickConsumer() {
            return event -> AxxHost.getInstance(project).showCurrent();
        }

        @Override
        public void dispose() {
            statusBar = null;
        }
    }
}
