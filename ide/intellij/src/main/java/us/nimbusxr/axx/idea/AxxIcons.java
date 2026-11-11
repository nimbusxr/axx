// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import com.intellij.openapi.util.IconLoader;

import org.jetbrains.annotations.NotNull;

import java.net.URL;
import java.util.Objects;

import javax.swing.Icon;

/**
 * The plugin's icons, each loaded by its URL. The IDE keeps the icons it showed, in its caches and
 * in the status bar, after the plugin unloads; an icon loaded through the plugin's class loader
 * ({@code IconLoader.getIcon(path, aClass)}) keeps that class loader, and the plugin could not
 * unload: an update would need a restart.
 */
public final class AxxIcons {
    /** axx's logo: the run configurations', and the language server's in the status bar. */
    public static final @NotNull Icon AXX = load("/icons/axx.svg");

    private AxxIcons() {}

    private static Icon load(String path) {
        URL url = Objects.requireNonNull(AxxIcons.class.getResource(path), path);
        return Objects.requireNonNull(IconLoader.findIcon(url), path);
    }
}
