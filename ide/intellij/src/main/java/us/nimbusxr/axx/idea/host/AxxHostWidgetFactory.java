// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import com.intellij.openapi.actionSystem.ActionPlaces;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.text.StringUtil;
import com.intellij.openapi.wm.CustomStatusBarWidget;
import com.intellij.openapi.wm.StatusBar;
import com.intellij.openapi.wm.StatusBarWidget;
import com.intellij.openapi.wm.StatusBarWidgetFactory;
import com.intellij.ui.ClickListener;
import com.intellij.ui.PopupHandler;
import com.intellij.ui.components.JBLabel;
import com.intellij.util.ui.JBUI;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.awt.event.MouseEvent;
import java.util.List;

import javax.swing.JComponent;
import javax.swing.SwingUtilities;

/**
 * The axx host's item in the status bar, once the IDE started one in the project: {@code axx host:}
 * and its state (ready; run 4 and its command, starting, running or cleaning up; waiting for your
 * approval; stopped; already running in a terminal), with where it listens, its services and their
 * sources on hover. A click shows the question that waits for an answer, else the run's tab; a
 * right click, a menu that restarts or stops the host ({@link #MENU}).
 */
public final class AxxHostWidgetFactory implements StatusBarWidgetFactory {
    /** The widget's ID, as plugin.xml declares the factory. */
    static final String ID = "us.nimbusxr.axx.idea.host";

    /** The menu a right click on the item shows, as plugin.xml declares it. */
    static final String MENU = "us.nimbusxr.axx.idea.HostWidget";

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

    /** The item: a label of its own, which hears both clicks. */
    static final class Widget implements CustomStatusBarWidget {
        private final Project project;
        private final JBLabel label = new JBLabel();

        Widget(Project project) {
            this.project = project;
            label.setBorder(JBUI.Borders.empty(0, 6));
            new ClickListener() {
                @Override
                public boolean onClick(@NotNull MouseEvent event, int clickCount) {
                    if (event.isPopupTrigger() || !SwingUtilities.isLeftMouseButton(event)) {
                        return false;
                    }
                    AxxHost.getInstance(project).showCurrent();
                    return true;
                }
            }.installOn(label);
            PopupHandler.installPopupMenu(label, MENU, ActionPlaces.STATUS_BAR_PLACE);
            update();
            project.getMessageBus()
                    .connect(this)
                    .subscribe(
                            AxxHost.Listener.TOPIC,
                            () ->
                                    ApplicationManager.getApplication()
                                            .invokeLater(this::update, project.getDisposed()));
        }

        /** Shows the host's state. On the UI thread. */
        void update() {
            AxxHost host = AxxHost.getInstance(project);
            label.setText(host.text());
            label.setToolTipText(tooltip(host.tooltip()));
        }

        private static @Nullable String tooltip(List<String> lines) {
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

        /** The label; for tests. */
        JBLabel label() {
            return label;
        }

        @Override
        public @NotNull String ID() {
            return ID;
        }

        @Override
        public @NotNull JComponent getComponent() {
            return label;
        }

        @Override
        public void install(@NotNull StatusBar statusBar) {}

        @Override
        public void dispose() {}
    }
}
