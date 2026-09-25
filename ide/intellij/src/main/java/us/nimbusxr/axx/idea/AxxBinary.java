// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import com.intellij.execution.ExecutionException;
import com.intellij.execution.configurations.PathEnvironmentVariableUtil;
import com.intellij.openapi.util.SystemInfo;
import com.intellij.util.EnvironmentUtil;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.nio.file.Path;
import java.util.List;

/** Finds the axx executable to run, following the "axx executable" setting. */
public final class AxxBinary {
    private AxxBinary() {}

    /**
     * The axx executable for a project.
     *
     * @param projectDir the project directory, where a relative setting starts
     * @param action what is about to be done, for the error, such as "run axx"
     * @throws ExecutionException when it cannot be found, saying where the plugin looked
     */
    public static @NotNull Path find(@NotNull Path projectDir, @NotNull String action)
            throws ExecutionException {
        String setting = AxxSettings.getInstance().getExecutable();
        Path home = Path.of(System.getProperty("user.home"));
        List<Path> installLocations =
                AxxExecutable.installLocations(
                        EnvironmentUtil::getValue, home, SystemInfo.isWindows);
        Path executable =
                AxxExecutable.resolve(setting, projectDir, AxxBinary::onPath, installLocations);
        if (executable == null) {
            throw new ExecutionException(
                    AxxExecutable.notFoundMessage(action, setting, installLocations, home));
        }
        return executable;
    }

    /**
     * Looks a command up on the PATH the IDE gives processes it starts (on macOS, the login
     * shell's PATH, even when the IDE was not started from a shell).
     */
    private static @Nullable Path onPath(@NotNull String command) {
        return AxxExecutable.findOnPath(
                command,
                PathEnvironmentVariableUtil.getPathVariableValue(),
                PathEnvironmentVariableUtil.getWindowsExecutableFileExtensions());
    }
}
