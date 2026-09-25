// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.execution.actions.ConfigurationContext;
import com.intellij.execution.actions.LazyRunConfigurationProducer;
import com.intellij.execution.configurations.ConfigurationFactory;
import com.intellij.openapi.util.Ref;
import com.intellij.psi.PsiElement;

import org.jetbrains.annotations.NotNull;

import us.nimbusxr.axx.idea.run.AxxRunConfiguration;
import us.nimbusxr.axx.idea.run.AxxRunConfigurationType;

import java.nio.file.Path;

/**
 * Creates axx run configurations from the editor, the gutter and the project view: a Feature,
 * Scenario, Scenario Outline or Examples row, a feature file, or a directory of them.
 */
public final class AxxRunConfigurationProducer
        extends LazyRunConfigurationProducer<AxxRunConfiguration> {

    @Override
    public @NotNull ConfigurationFactory getConfigurationFactory() {
        return AxxRunConfigurationType.getInstance().getFactory();
    }

    @Override
    protected boolean setupConfigurationFromContext(
            @NotNull AxxRunConfiguration configuration,
            @NotNull ConfigurationContext context,
            @NotNull Ref<PsiElement> sourceElement) {
        AxxRunSelection selection = AxxRunSelection.of(context.getPsiLocation());
        if (selection == null) {
            return false;
        }
        sourceElement.set(selection.element());
        configuration.setTargets(selection.targets());
        configuration.setWorkingDirectory(selection.workingDirectory().toString());
        configuration.setName(selection.name());
        return true;
    }

    @Override
    public boolean isConfigurationFromContext(
            @NotNull AxxRunConfiguration configuration, @NotNull ConfigurationContext context) {
        AxxRunSelection selection = AxxRunSelection.of(context.getPsiLocation());
        return selection != null
                && selection.targets().equals(configuration.getTargets())
                && !configuration.getWorkingDirectory().isEmpty()
                && Path.of(configuration.getWorkingDirectory()).normalize()
                        .equals(selection.workingDirectory().normalize());
    }
}
