// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.fileChooser.FileChooserDescriptorFactory;
import com.intellij.openapi.options.Configurable;
import com.intellij.openapi.ui.TextFieldWithBrowseButton;
import com.intellij.ui.JBIntSpinner;
import com.intellij.util.ui.FormBuilder;

import org.jetbrains.annotations.Nullable;

import java.awt.FlowLayout;

import javax.swing.JComponent;
import javax.swing.JLabel;
import javax.swing.JPanel;

/**
 * <em>Settings | Tools | axx</em>: where to find the axx executable, for the language server and
 * axx run configurations, and how much runs that watch the browsers slow down.
 */
public final class AxxConfigurable implements Configurable {
    private static final int MAX_SLOWDOWN_MILLIS = 10_000;

    private @Nullable TextFieldWithBrowseButton executable;
    private @Nullable JBIntSpinner watchSlowdown;

    @Override
    public String getDisplayName() {
        return "axx";
    }

    @Override
    public @Nullable JComponent createComponent() {
        executable = new TextFieldWithBrowseButton();
        executable.addBrowseFolderListener(
                null, FileChooserDescriptorFactory.singleFile().withTitle("axx Executable"));
        watchSlowdown =
                new JBIntSpinner(
                        AxxSettings.DEFAULT_WATCH_SLOWDOWN_MILLIS, 0, MAX_SLOWDOWN_MILLIS, 50);
        JPanel slowdown = new JPanel(new FlowLayout(FlowLayout.LEFT, 0, 0));
        slowdown.add(watchSlowdown);
        slowdown.add(new JLabel(" ms"));
        return FormBuilder.createFormBuilder()
                .addLabeledComponent("axx executable:", executable)
                .addTooltip(
                        "The axx command, found on PATH or where axx's installers put it, or the"
                                + " path to the axx binary. A relative path starts at the project"
                                + " directory.")
                .addLabeledComponent("Slowdown when watching the browsers:", slowdown)
                .addTooltip(
                        "Runs that watch the browsers wait this long after every browser action,"
                                + " so you can follow what happens. 0 adds no wait.")
                .addComponentFillVertically(new JPanel(), 0)
                .getPanel();
    }

    @Override
    public boolean isModified() {
        AxxSettings settings = AxxSettings.getInstance();
        return (executable != null
                        && !AxxExecutable.normalize(executable.getText())
                                .equals(settings.getExecutable()))
                || (watchSlowdown != null
                        && watchSlowdown.getNumber() != settings.getWatchSlowdownMillis());
    }

    @Override
    public void apply() {
        AxxSettings settings = AxxSettings.getInstance();
        if (watchSlowdown != null) {
            settings.setWatchSlowdownMillis(watchSlowdown.getNumber());
        }
        if (executable == null
                || AxxExecutable.normalize(executable.getText()).equals(settings.getExecutable())) {
            return;
        }
        settings.setExecutable(executable.getText());
        ApplicationManager.getApplication()
                .getMessageBus()
                .syncPublisher(AxxSettings.Listener.TOPIC)
                .executableChanged();
    }

    @Override
    public void reset() {
        if (executable != null) {
            executable.setText(AxxSettings.getInstance().getExecutable());
        }
        if (watchSlowdown != null) {
            watchSlowdown.setNumber(AxxSettings.getInstance().getWatchSlowdownMillis());
        }
    }

    @Override
    public void disposeUIResources() {
        executable = null;
        watchSlowdown = null;
    }
}
