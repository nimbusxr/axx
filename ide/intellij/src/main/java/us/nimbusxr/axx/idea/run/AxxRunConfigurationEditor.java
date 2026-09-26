// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.openapi.fileChooser.FileChooserDescriptorFactory;
import com.intellij.openapi.options.SettingsEditor;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.ui.TextFieldWithBrowseButton;
import com.intellij.ui.RawCommandLineEditor;
import com.intellij.ui.components.JBCheckBox;
import com.intellij.util.ui.FormBuilder;

import org.jetbrains.annotations.NotNull;

import javax.swing.JComponent;
import javax.swing.JPanel;

/** The settings of an axx run configuration. */
final class AxxRunConfigurationEditor extends SettingsEditor<AxxRunConfiguration> {
    private final RawCommandLineEditor targets = new RawCommandLineEditor();
    private final RawCommandLineEditor arguments = new RawCommandLineEditor();
    private final TextFieldWithBrowseButton workingDirectory = new TextFieldWithBrowseButton();
    private final JBCheckBox watchBrowsers = new JBCheckBox("Watch the browsers");
    private final JPanel panel;

    AxxRunConfigurationEditor(@NotNull Project project) {
        workingDirectory.addBrowseFolderListener(
                project, FileChooserDescriptorFactory.singleDir().withTitle("Working Directory"));
        panel =
                FormBuilder.createFormBuilder()
                        .addLabeledComponent("Targets:", targets)
                        .addTooltip(
                                "Feature files or directories, each optionally with :line (a"
                                        + " Feature, Scenario or Examples row line). Empty runs the"
                                        + " suite's run.paths.")
                        .addLabeledComponent("Arguments:", arguments)
                        .addTooltip("More axx run arguments, such as --tags \"@smoke\".")
                        .addLabeledComponent("Working directory:", workingDirectory)
                        .addTooltip(
                                "Where axx runs and targets start. Empty: the directory of the"
                                        + " axx.yaml above the first target.")
                        .addComponent(watchBrowsers)
                        .addTooltip(
                                "Every run opens the web pack's browsers in windows on the"
                                        + " desktop as they go, one scenario at a time, slowed down"
                                        + " (Settings | Tools | axx). Watch does this for one run.")
                        .addComponentFillVertically(new JPanel(), 0)
                        .getPanel();
    }

    @Override
    protected void resetEditorFrom(@NotNull AxxRunConfiguration configuration) {
        targets.setText(configuration.getTargetsText());
        arguments.setText(configuration.getArguments());
        workingDirectory.setText(configuration.getWorkingDirectory());
        watchBrowsers.setSelected(configuration.isWatchBrowsers());
    }

    @Override
    protected void applyEditorTo(@NotNull AxxRunConfiguration configuration) {
        configuration.setTargetsText(targets.getText());
        configuration.setArguments(arguments.getText());
        configuration.setWorkingDirectory(workingDirectory.getText());
        configuration.setWatchBrowsers(watchBrowsers.isSelected());
    }

    @Override
    protected @NotNull JComponent createEditor() {
        return panel;
    }
}
