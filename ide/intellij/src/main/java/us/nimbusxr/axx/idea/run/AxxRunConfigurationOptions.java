// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.configurations.LocatableRunConfigurationOptions;
import com.intellij.openapi.components.StoredProperty;

import org.jetbrains.annotations.NotNull;

/** What an axx run configuration stores. */
public final class AxxRunConfigurationOptions extends LocatableRunConfigurationOptions {
    private final StoredProperty<String> targets = string("").provideDelegate(this, "targets");
    private final StoredProperty<String> arguments = string("").provideDelegate(this, "arguments");
    private final StoredProperty<String> workingDirectory =
            string("").provideDelegate(this, "workingDirectory");

    /** The targets, as a command line (quoted where needed). */
    public @NotNull String getTargets() {
        return nonNull(targets.getValue(this));
    }

    public void setTargets(@NotNull String value) {
        targets.setValue(this, value);
    }

    /** Extra {@code axx run} arguments, as a command line. */
    public @NotNull String getArguments() {
        return nonNull(arguments.getValue(this));
    }

    public void setArguments(@NotNull String value) {
        arguments.setValue(this, value);
    }

    /** The directory axx runs in; empty for the axx project of the first target. */
    public @NotNull String getWorkingDirectory() {
        return nonNull(workingDirectory.getValue(this));
    }

    public void setWorkingDirectory(@NotNull String value) {
        workingDirectory.setValue(this, value);
    }

    private static String nonNull(String value) {
        return value == null ? "" : value;
    }
}
