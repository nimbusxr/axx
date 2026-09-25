// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.openapi.fileChooser.FileChooserDescriptorFactory;
import com.intellij.openapi.options.SettingsEditor;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.ui.TextFieldWithBrowseButton;
import com.intellij.ui.RawCommandLineEditor;
import com.intellij.util.ui.FormBuilder;

import org.jetbrains.annotations.NotNull;

import javax.swing.JComponent;
import javax.swing.JPanel;

/** The settings of an axx run configuration. */
final class AxxRunConfigurationEditor extends SettingsEditor<AxxRunConfiguration> {
    private final RawCommandLineEditor targets = new RawCommandLineEditor();
    private final RawCommandLineEditor arguments = new RawCommandLineEditor();
    private final TextFieldWithBrowseButton workingDirectory = new TextFieldWithBrowseButton();
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
                        .addComponentFillVertically(new JPanel(), 0)
                        .getPanel();
    }

    @Override
    protected void resetEditorFrom(@NotNull AxxRunConfiguration configuration) {
        targets.setText(configuration.getTargetsText());
        arguments.setText(configuration.getArguments());
        workingDirectory.setText(configuration.getWorkingDirectory());
    }

    @Override
    protected void applyEditorTo(@NotNull AxxRunConfiguration configuration) {
        configuration.setTargetsText(targets.getText());
        configuration.setArguments(arguments.getText());
        configuration.setWorkingDirectory(workingDirectory.getText());
    }

    @Override
    protected @NotNull JComponent createEditor() {
        return panel;
    }
}
