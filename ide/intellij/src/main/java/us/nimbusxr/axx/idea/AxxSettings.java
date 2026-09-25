// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.components.PersistentStateComponent;
import com.intellij.openapi.components.RoamingType;
import com.intellij.openapi.components.Service;
import com.intellij.openapi.components.State;
import com.intellij.openapi.components.Storage;
import com.intellij.util.messages.Topic;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

/**
 * The plugin's settings (<em>Settings | Tools | axx</em>). The executable path depends on the
 * machine, so the settings do not roam.
 */
@Service(Service.Level.APP)
@State(
        name = "us.nimbusxr.axx.idea.AxxSettings",
        storages = @Storage(value = "axx.xml", roamingType = RoamingType.DISABLED))
public final class AxxSettings implements PersistentStateComponent<AxxSettings.StoredState> {

    /** The persisted form of the settings. */
    public static final class StoredState {
        /** The axx command on PATH, or a path to the binary (see {@link AxxExecutable}). */
        public String executable = AxxExecutable.DEFAULT;
    }

    /** Hears about changed settings, on the application message bus. */
    public interface Listener {
        @Topic.AppLevel
        Topic<Listener> TOPIC = new Topic<>(Listener.class, Topic.BroadcastDirection.NONE);

        /** The axx executable setting changed. */
        void executableChanged();
    }

    private StoredState state = new StoredState();

    public static @NotNull AxxSettings getInstance() {
        return ApplicationManager.getApplication().getService(AxxSettings.class);
    }

    @Override
    public @NotNull StoredState getState() {
        return state;
    }

    @Override
    public void loadState(@NotNull StoredState state) {
        this.state = state;
    }

    /** The configured axx command or path; {@code axx} unless set. */
    public @NotNull String getExecutable() {
        return AxxExecutable.normalize(state.executable);
    }

    public void setExecutable(@Nullable String executable) {
        state.executable = AxxExecutable.normalize(executable);
    }
}
