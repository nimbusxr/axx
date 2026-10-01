// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.openapi.fileChooser.FileChooserDescriptorFactory;
import com.intellij.openapi.options.SettingsEditor;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.ui.TextFieldWithBrowseButton;
import com.intellij.ui.CheckBoxList;
import com.intellij.ui.CheckBoxListListener;
import com.intellij.ui.RawCommandLineEditor;
import com.intellij.ui.ToolbarDecorator;
import com.intellij.ui.components.JBCheckBox;
import com.intellij.util.ui.FormBuilder;

import org.jetbrains.annotations.NotNull;

import java.util.ArrayList;
import java.util.List;

import javax.swing.JComponent;
import javax.swing.JPanel;
import javax.swing.event.ListDataEvent;
import javax.swing.event.ListDataListener;

/** The settings of an axx run configuration. */
final class AxxRunConfigurationEditor extends SettingsEditor<AxxRunConfiguration> {
    private final RawCommandLineEditor targets = new RawCommandLineEditor();
    private final CheckBoxList<String> profiles = new CheckBoxList<>();
    private final RawCommandLineEditor arguments = new RawCommandLineEditor();
    private final TextFieldWithBrowseButton workingDirectory = new TextFieldWithBrowseButton();
    private final JBCheckBox watchBrowsers = new JBCheckBox("Watch the browsers");
    private final JPanel panel;

    AxxRunConfigurationEditor(@NotNull Project project) {
        workingDirectory.addBrowseFolderListener(
                project, FileChooserDescriptorFactory.singleDir().withTitle("Working Directory"));
        profiles.setVisibleRowCount(4);
        profiles.getEmptyText().setText("No profiles in this project's axx.yaml");
        profiles.setCheckBoxListListener(
                new CheckBoxListListener() {
                    @Override
                    public void checkBoxSelectionChanged(int index, boolean value) {
                        fireEditorStateChanged();
                    }
                });
        profiles.getModel()
                .addListDataListener(
                        new ListDataListener() {
                            @Override
                            public void intervalAdded(ListDataEvent e) {
                                fireEditorStateChanged();
                            }

                            @Override
                            public void intervalRemoved(ListDataEvent e) {
                                fireEditorStateChanged();
                            }

                            @Override
                            public void contentsChanged(ListDataEvent e) {
                                fireEditorStateChanged();
                            }
                        });
        // Up and down set the order the profiles apply in.
        JPanel profilesPanel =
                ToolbarDecorator.createDecorator(profiles)
                        .disableAddAction()
                        .disableRemoveAction()
                        .createPanel();
        panel =
                FormBuilder.createFormBuilder()
                        .addLabeledComponent("Targets:", targets)
                        .addTooltip(
                                "Feature files or directories, each optionally with :line (a"
                                        + " Feature, Scenario or Examples row line). Empty runs the"
                                        + " suite's run.paths.")
                        .addLabeledComponent("Profiles:", profilesPanel)
                        .addTooltip(
                                "The profiles to apply, in this order (--profile): profiles.<name>"
                                        + " in axx.yaml and axx.<name>.yaml files. Set them in the"
                                        + " axx configuration template to start every run from the"
                                        + " project view, the gutter and the editor with them.")
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
        // The chosen profiles first, in their order, then the project's others.
        List<String> chosen = configuration.getProfiles();
        List<String> all = new ArrayList<>(chosen);
        for (String profile : configuration.availableProfiles()) {
            if (!all.contains(profile)) {
                all.add(profile);
            }
        }
        profiles.clear();
        for (String profile : all) {
            profiles.addItem(profile, profile, chosen.contains(profile));
        }
        arguments.setText(configuration.getArguments());
        workingDirectory.setText(configuration.getWorkingDirectory());
        watchBrowsers.setSelected(configuration.isWatchBrowsers());
    }

    @Override
    protected void applyEditorTo(@NotNull AxxRunConfiguration configuration) {
        configuration.setTargetsText(targets.getText());
        configuration.setProfiles(chosenProfiles());
        configuration.setArguments(arguments.getText());
        configuration.setWorkingDirectory(workingDirectory.getText());
        configuration.setWatchBrowsers(watchBrowsers.isSelected());
    }

    /** The checked profiles, in the list's order. */
    private @NotNull List<String> chosenProfiles() {
        List<String> out = new ArrayList<>();
        for (int i = 0; i < profiles.getItemsCount(); i++) {
            String profile = profiles.getItemAt(i);
            if (profile != null && profiles.isItemSelected(i)) {
                out.add(profile);
            }
        }
        return out;
    }

    @Override
    protected @NotNull JComponent createEditor() {
        return panel;
    }
}
