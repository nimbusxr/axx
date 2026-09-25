// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.configurations.ConfigurationFactory;
import com.intellij.execution.configurations.ConfigurationTypeBase;
import com.intellij.execution.configurations.ConfigurationTypeUtil;
import com.intellij.execution.configurations.RunConfiguration;
import com.intellij.openapi.components.BaseState;
import com.intellij.openapi.project.DumbAware;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.IconLoader;

import org.jetbrains.annotations.NotNull;

/** The "axx" run configuration type: runs scenarios with {@code axx run}. */
public final class AxxRunConfigurationType extends ConfigurationTypeBase implements DumbAware {
    /** The configuration type ID, as run configuration files store it. */
    public static final String ID = "AxxRunConfiguration";

    private final ConfigurationFactory factory;

    public AxxRunConfigurationType() {
        super(
                ID,
                "axx",
                "Runs axx scenarios and shows them as tests",
                IconLoader.getIcon("/icons/axx.svg", AxxRunConfigurationType.class));
        factory = new Factory(this);
        addFactory(factory);
    }

    public static @NotNull AxxRunConfigurationType getInstance() {
        return ConfigurationTypeUtil.findConfigurationType(AxxRunConfigurationType.class);
    }

    public @NotNull ConfigurationFactory getFactory() {
        return factory;
    }

    private static final class Factory extends ConfigurationFactory {
        Factory(AxxRunConfigurationType type) {
            super(type);
        }

        @Override
        public @NotNull String getId() {
            return "axx";
        }

        @Override
        public @NotNull RunConfiguration createTemplateConfiguration(@NotNull Project project) {
            return new AxxRunConfiguration(project, this, "axx");
        }

        @Override
        public Class<? extends BaseState> getOptionsClass() {
            return AxxRunConfigurationOptions.class;
        }

        @Override
        public boolean isEditableInDumbMode() {
            return true;
        }
    }
}
