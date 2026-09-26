// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.intellij.execution.testframework.AbstractTestProxy;
import com.intellij.execution.ui.ConsoleViewContentType;
import com.intellij.openapi.util.Key;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.File;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * A trace or video a scenario kept, on the scenario's node in the test tree: its console shows the
 * file with a link that opens it, and the node's context menu has <em>Open Trace</em> or <em>Play
 * Video</em>.
 *
 * @param kind a trace or a video
 * @param path the file
 * @param label the scenario's name, for the tab that shows the file
 * @param viewerFolder the folder of the trace viewer's files the run announced; null for none
 */
public record KeptFile(
        @NotNull Kind kind,
        @NotNull Path path,
        @NotNull String label,
        @Nullable Path viewerFolder) {

    private static final Key<List<KeptFile>> KEPT = Key.create("us.nimbusxr.axx.keptFiles");

    /** What a scenario kept. */
    public enum Kind {
        TRACE("Trace", "Open trace"),
        VIDEO("Video", "Play video");

        private final String label;
        private final String link;

        Kind(String label, String link) {
            this.label = label;
            this.link = link;
        }
    }

    /**
     * Adds the file to a test node, and to its console: the file, relative to the working
     * directory when it is inside it, and the link that opens it.
     */
    public void attachTo(@NotNull AbstractTestProxy node, @Nullable Path workingDirectory) {
        node.putUserDataIfAbsent(KEPT, new CopyOnWriteArrayList<>()).add(this);
        String shown = shown(workingDirectory);
        node.addLast(
                printer -> {
                    printer.print(
                            kind.label + ": " + shown + "  ", ConsoleViewContentType.NORMAL_OUTPUT);
                    printer.printHyperlink(kind.link, project -> AxxWebViewer.open(project, this));
                    printer.print("\n", ConsoleViewContentType.NORMAL_OUTPUT);
                });
    }

    /** The files of a kind a test node kept, or its scenario, for a step. */
    public static @NotNull List<KeptFile> of(@Nullable AbstractTestProxy node, @NotNull Kind kind) {
        List<KeptFile> found = new ArrayList<>();
        for (AbstractTestProxy p = node; p != null && found.isEmpty(); p = p.getParent()) {
            List<KeptFile> kept = p.getUserData(KEPT);
            if (kept != null) {
                for (KeptFile file : kept) {
                    if (file.kind == kind) {
                        found.add(file);
                    }
                }
            }
        }
        return found;
    }

    private String shown(@Nullable Path workingDirectory) {
        if (workingDirectory != null && path.startsWith(workingDirectory)) {
            return workingDirectory.relativize(path).toString().replace(File.separatorChar, '/');
        }
        return path.toString();
    }
}
