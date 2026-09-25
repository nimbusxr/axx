// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.fileChooser.FileChooserDescriptorFactory;
import com.intellij.openapi.options.Configurable;
import com.intellij.openapi.ui.TextFieldWithBrowseButton;
import com.intellij.util.ui.FormBuilder;

import org.jetbrains.annotations.Nullable;

import javax.swing.JComponent;
import javax.swing.JPanel;

/**
 * <em>Settings | Tools | axx</em>: where to find the axx executable, for the language server and
 * axx run configurations.
 */
public final class AxxConfigurable implements Configurable {
    private @Nullable TextFieldWithBrowseButton executable;

    @Override
    public String getDisplayName() {
        return "axx";
    }

    @Override
    public @Nullable JComponent createComponent() {
        executable = new TextFieldWithBrowseButton();
        executable.addBrowseFolderListener(
                null, FileChooserDescriptorFactory.singleFile().withTitle("axx Executable"));
        return FormBuilder.createFormBuilder()
                .addLabeledComponent("axx executable:", executable)
                .addTooltip(
                        "The axx command, found on PATH or where axx's installers put it, or the"
                                + " path to the axx binary. A relative path starts at the project"
                                + " directory.")
                .addComponentFillVertically(new JPanel(), 0)
                .getPanel();
    }

    @Override
    public boolean isModified() {
        return executable != null
                && !AxxExecutable.normalize(executable.getText())
                        .equals(AxxSettings.getInstance().getExecutable());
    }

    @Override
    public void apply() {
        if (executable == null) {
            return;
        }
        AxxSettings.getInstance().setExecutable(executable.getText());
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
    }

    @Override
    public void disposeUIResources() {
        executable = null;
    }
}
