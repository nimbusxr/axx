// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.testframework.TestConsoleProperties;
import com.intellij.execution.testframework.sm.runner.GeneralTestEventsProcessor;
import com.intellij.execution.testframework.sm.runner.OutputToGeneralTestEventsConverter;
import com.intellij.execution.testframework.sm.runner.SMTRunnerEventsAdapter;
import com.intellij.execution.testframework.sm.runner.SMTestProxy;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.Key;

import jetbrains.buildServer.messages.serviceMessages.ServiceMessageVisitor;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.IdeLine;
import us.nimbusxr.axx.idea.web.AxxPausedRuns;
import us.nimbusxr.axx.idea.web.AxxWebViewer;
import us.nimbusxr.axx.idea.web.IdeAnnouncement;
import us.nimbusxr.axx.idea.web.KeptFile;
import us.nimbusxr.axx.idea.web.ScenarioPause;

import java.nio.file.Path;
import java.text.ParseException;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Reads axx's output for the test runner: the TeamCity service messages, as the platform does, and
 * the lines packs print for the IDE (see {@link IdeAnnouncement} and {@link ScenarioPause}), which
 * it acts on instead of printing them. It adds each trace and video to its scenario's node (see
 * {@link KeptFile}), and tells the project's {@link AxxPausedRuns} when a scenario pauses and
 * resumes, and when the run ends. Lines are read in order, and a scenario's node is created as its
 * start is read, so it exists when its files are announced.
 */
final class AxxTestEventsConverter extends OutputToGeneralTestEventsConverter {
    private final @Nullable Path workingDirectory;
    private final @Nullable Project project;
    // The scenarios' nodes, by the location axx reports for them (file:///abs/path:line).
    private final Map<String, SMTestProxy> scenarios = new ConcurrentHashMap<>();
    private volatile @Nullable SMTestProxy root;
    private volatile @Nullable Path viewerFolder;

    /** @param workingDirectory the directory axx runs in, or null when unknown */
    AxxTestEventsConverter(
            @NotNull String testFrameworkName,
            @NotNull TestConsoleProperties properties,
            @Nullable Path workingDirectory) {
        super(testFrameworkName, properties);
        this.workingDirectory = workingDirectory;
        this.project = properties.getProject();
    }

    @Override
    public void setProcessor(@Nullable GeneralTestEventsProcessor processor) {
        super.setProcessor(processor);
        if (processor != null) {
            processor.addEventsListener(
                    new SMTRunnerEventsAdapter() {
                        @Override
                        public void onSuiteStarted(@NotNull SMTestProxy suite) {
                            SMTestProxy parent = suite.getParent();
                            String location = suite.getLocationUrl();
                            root = suite.getRoot();
                            // Features are the root's; scenarios are theirs.
                            if (parent != null && parent.getParent() != null && location != null) {
                                scenarios.put(location, suite);
                            }
                        }
                    });
        }
    }

    @Override
    protected boolean processServiceMessages(
            @NotNull String text,
            @NotNull Key<?> outputType,
            @NotNull ServiceMessageVisitor visitor)
            throws ParseException {
        boolean marked = text.contains(IdeLine.MARKER);
        ScenarioPause pause = marked ? ScenarioPause.parse(text) : null;
        if (pause != null) {
            paused(pause);
            return true;
        }
        IdeAnnouncement announcement = marked ? IdeAnnouncement.parse(text) : null;
        if (announcement == null) {
            return super.processServiceMessages(text, outputType, visitor);
        }
        switch (announcement.kind()) {
            case TRACE_VIEWER -> {
                viewerFolder = announcement.path();
                AxxWebViewer.rememberViewerFolder(announcement.path());
            }
            case TRACE, VIDEO -> keep(announcement);
        }
        return true;
    }

    /** Tells the project's paused runs that this run's scenario paused or resumed. */
    private void paused(ScenarioPause pause) {
        if (project == null || project.isDisposed()) {
            return;
        }
        AxxPausedRuns runs = AxxPausedRuns.getInstance(project);
        if (pause.paused()) {
            runs.paused(this, pause);
        } else {
            runs.resumed(this, pause.url());
        }
    }

    @Override
    public void flushBufferOnProcessTermination(int exitCode) {
        super.flushBufferOnProcessTermination(exitCode);
        ended();
    }

    @Override
    public void dispose() {
        ended();
        super.dispose();
    }

    /** Tells the project's paused runs, if a scenario of this run paused, that the run ended. */
    private void ended() {
        AxxPausedRuns runs = AxxPausedRuns.getIfCreated(project);
        if (runs != null) {
            runs.ended(this);
        }
    }

    /** Adds a trace or video to its scenario's node, or else the root's. */
    private void keep(IdeAnnouncement announcement) {
        String location =
                RunTargets.findLocation(
                        scenarios.keySet(),
                        workingDirectory,
                        announcement.file(),
                        announcement.line());
        SMTestProxy scenario = location == null ? null : scenarios.get(location);
        SMTestProxy node = scenario != null ? scenario : root;
        KeptFile file =
                new KeptFile(
                        announcement.kind() == IdeAnnouncement.Kind.TRACE
                                ? KeptFile.Kind.TRACE
                                : KeptFile.Kind.VIDEO,
                        announcement.path(),
                        scenario != null
                                ? withoutKeyword(scenario.getName())
                                : announcement.path().getFileName().toString(),
                        viewerFolder);
        if (node != null) {
            file.attachTo(node, workingDirectory);
        }
    }

    /** A scenario's name without its keyword, such as "Register a parcel". */
    private static String withoutKeyword(String name) {
        int colon = name.indexOf(": ");
        return colon > 0 ? name.substring(colon + 2) : name;
    }
}
