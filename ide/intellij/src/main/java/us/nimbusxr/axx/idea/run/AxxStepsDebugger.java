// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.RunManager;
import com.intellij.execution.RunnerAndConfigurationSettings;
import com.intellij.execution.configurations.ConfigurationType;
import com.intellij.execution.configurations.ConfigurationTypeUtil;
import com.intellij.execution.configurations.RunConfiguration;
import com.intellij.execution.configurations.UnknownConfigurationType;
import com.intellij.openapi.diagnostic.Logger;
import com.intellij.openapi.project.Project;

import org.jdom.Element;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.DebugRequest;

/**
 * The Go debugger that attaches to {@code axx run --debug-steps}: the {@code "Debugger:
 * axx-steps"} run configuration, a Go Remote configuration. When axx, running under Delve, asks for
 * it ({@code [AXX-IDE] debug-attach-request name=axx-steps ...}), the plugin starts it like any
 * other debugger configuration (see {@code DebugAutoStartListener}).
 */
final class AxxStepsDebugger {
    /** The configuration axx asks for. */
    static final String CONFIGURATION = DebugRequest.CONFIGURATION_PREFIX + "axx-steps";

    /** The Go Remote configuration type of the Go plugin (and GoLand). */
    static final String GO_REMOTE_TYPE = "GoRemoteDebugConfigurationType";

    /** Delve's port for {@code --debug-steps}, as axx and the Go Remote configuration default. */
    static final int DEFAULT_PORT = 2345;

    static final String NO_GO_DEBUGGER =
            "Stopping at breakpoints in axx step code needs a Go debugger: GoLand, or IntelliJ IDEA"
                    + " with the Go plugin. The scenarios run without it.";

    private static final Logger LOG = Logger.getInstance(AxxStepsDebugger.class);

    private AxxStepsDebugger() {}

    /**
     * Makes sure {@code "Debugger: axx-steps"} exists, creating a Go Remote configuration for
     * {@code 127.0.0.1:2345} when there is none. Call on the UI thread.
     *
     * @return the Delve port to pass to {@code --debug-steps}, or null when there is no Go
     *     debugger to attach
     */
    static @Nullable Integer prepare(@NotNull Project project) {
        RunManager runManager = RunManager.getInstance(project);
        RunnerAndConfigurationSettings existing = runManager.findConfigurationByName(CONFIGURATION);
        if (existing != null && !(existing.getType() instanceof UnknownConfigurationType)) {
            return portOf(existing.getConfiguration());
        }
        ConfigurationType goRemote = ConfigurationTypeUtil.findConfigurationType(GO_REMOTE_TYPE);
        if (goRemote == null || goRemote.getConfigurationFactories().length == 0) {
            return null;
        }
        if (existing != null) {
            // Written before the Go plugin was installed; replace it with a working one.
            runManager.removeConfiguration(existing);
        }
        RunnerAndConfigurationSettings settings =
                runManager.createConfiguration(
                        CONFIGURATION, goRemote.getConfigurationFactories()[0]);
        try {
            settings.getConfiguration().readExternal(remoteElement("127.0.0.1", DEFAULT_PORT));
        } catch (RuntimeException e) {
            LOG.warn("axx: cannot set up " + CONFIGURATION + ", using its defaults", e);
        }
        runManager.addConfiguration(settings);
        LOG.info("axx: created the " + CONFIGURATION + " run configuration");
        return DEFAULT_PORT;
    }

    /**
     * A Go Remote configuration's settings: the platform's remote debug configurations store the
     * host and port as attributes of the configuration element.
     */
    static @NotNull Element remoteElement(@NotNull String host, int port) {
        return new Element("configuration")
                .setAttribute("host", host)
                .setAttribute("port", String.valueOf(port));
    }

    /** The port a debugger configuration attaches to, or {@link #DEFAULT_PORT} when unknown. */
    static int portOf(@NotNull RunConfiguration configuration) {
        Element element = new Element("configuration");
        try {
            configuration.writeExternal(element);
        } catch (RuntimeException e) {
            return DEFAULT_PORT;
        }
        String port = element.getAttributeValue("port");
        for (Element option : element.getChildren("option")) {
            if (port == null && "PORT".equals(option.getAttributeValue("name"))) {
                port = option.getAttributeValue("value");
            }
        }
        try {
            int value = port == null ? DEFAULT_PORT : Integer.parseInt(port.strip());
            return value > 0 && value <= 65535 ? value : DEFAULT_PORT;
        } catch (NumberFormatException e) {
            return DEFAULT_PORT;
        }
    }
}
