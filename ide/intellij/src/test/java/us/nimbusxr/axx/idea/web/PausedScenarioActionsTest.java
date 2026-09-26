// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.intellij.notification.Notification;
import com.intellij.notification.Notifications;
import com.intellij.openapi.actionSystem.ActionManager;
import com.intellij.openapi.actionSystem.AnAction;
import com.intellij.openapi.actionSystem.AnActionEvent;
import com.intellij.openapi.actionSystem.KeyboardShortcut;
import com.intellij.openapi.actionSystem.Presentation;
import com.intellij.openapi.actionSystem.ex.ActionUtil;
import com.intellij.openapi.editor.LogicalPosition;
import com.intellij.openapi.keymap.Keymap;
import com.intellij.openapi.keymap.ex.KeymapManagerEx;
import com.intellij.openapi.util.io.FileUtil;
import com.intellij.openapi.wm.StatusBar;
import com.intellij.openapi.wm.StatusBarInfo;
import com.intellij.testFramework.PlatformTestUtil;
import com.intellij.testFramework.TestActionEvent;
import com.intellij.testFramework.fixtures.BasePlatformTestCase;

import org.jetbrains.annotations.NotNull;

import us.nimbusxr.axx.idea.web.FakePausedScenario.Request;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;

import javax.swing.KeyStroke;

/**
 * The editor side of a paused scenario, against a fake one: the step under the caret is
 * highlighted, <em>Run Step in Paused Scenario</em> runs it with its table, and <em>Insert Recorded
 * Steps</em> inserts what was recorded.
 */
public class PausedScenarioActionsTest extends BasePlatformTestCase {
    private static final String RUN_STEP = "us.nimbusxr.axx.idea.RunStepInPausedScenario";
    private static final String INSERT = "us.nimbusxr.axx.idea.InsertRecordedSteps";
    private static final String NOWHERE = "http://127.0.0.1:1/3f9c0a/";

    private final List<Object> runs = new ArrayList<>();
    private final List<FakePausedScenario> scenarios = new ArrayList<>();

    @Override
    protected void tearDown() throws Exception {
        try {
            for (Object run : runs) {
                AxxPausedRuns.getInstance(getProject()).ended(run);
            }
            scenarios.forEach(FakePausedScenario::close);
        } catch (Throwable e) {
            addSuppressedException(e);
        } finally {
            super.tearDown();
        }
    }

    public void testRunStepShortcutIsFreeInTheBundledKeymaps() {
        KeyStroke keyStroke = KeyStroke.getKeyStroke("ctrl alt shift R");
        for (String name :
                List.of(
                        "$default",
                        "Default for XWin",
                        "Default for GNOME",
                        "Default for KDE",
                        "Mac OS X 10.5+",
                        "Mac OS X",
                        "macOS System Shortcuts")) {
            Keymap keymap = KeymapManagerEx.getInstanceEx().getKeymap(name);
            assertNotNull(name, keymap);
            assertEquals(
                    name,
                    List.of(new KeyboardShortcut(keyStroke, null)),
                    List.of(keymap.getShortcuts(RUN_STEP)));
            assertEquals(name, List.of(RUN_STEP), List.of(keymap.getActionIds(keyStroke)));
        }
    }

    public void testActionsNeedAPausedScenarioOrRecordedSteps() {
        myFixture.configureByText("shop-portal.feature", feature(4));
        Object run = pause(NOWHERE, Path.of(FileUtil.getTempDirectory(), "missing.txt"));
        assertTrue(update(RUN_STEP).isEnabledAndVisible());
        assertTrue(update(INSERT).isEnabledAndVisible());

        myFixture.getEditor().getCaretModel().moveToLogicalPosition(new LogicalPosition(2, 4));
        Presentation onScenarioLine = update(RUN_STEP);
        assertTrue(onScenarioLine.isVisible());
        assertFalse(onScenarioLine.isEnabled());

        AxxPausedRuns.getInstance(getProject()).ended(run);
        assertFalse(update(RUN_STEP).isVisible());
        assertFalse(update(INSERT).isVisible()); // the recording file does not exist

        myFixture.configureByText("notes.txt", "When the page \"/quote\" is opened\n");
        pause(NOWHERE, null);
        assertFalse(update(RUN_STEP).isVisible());
        assertFalse(update(INSERT).isVisible());
    }

    public void testStepUnderTheCaretIsHighlighted() throws Exception {
        FakePausedScenario scenario = scenario();
        scenario.answer("POST", "highlight", 200, "the page");
        BlockingQueue<String> status = new LinkedBlockingQueue<>();
        getProject()
                .getMessageBus()
                .connect(getTestRootDisposable())
                .subscribe(
                        StatusBar.Info.INSTANCE.getTOPIC(),
                        new StatusBarInfo() {
                            @Override
                            public void setInfo(String s) {
                                status.add(String.valueOf(s));
                            }

                            @Override
                            public void setInfo(String s, String requestor) {
                                status.add(String.valueOf(s));
                            }

                            @Override
                            public String getInfo() {
                                return null;
                            }
                        });
        myFixture.configureByText("shop-portal.feature", feature(4));

        pause(scenario.url, null);
        // The step at the caret when the scenario paused.
        assertEquals("When the page \"/quote\" is opened", nextRequest(scenario, "highlight").body());
        waitFor(() -> status.contains("axx: the page"));

        scenario.answer(
                "POST", "highlight", 404, "No text \"Your quote\" on the page; the page shows ...");
        myFixture.getEditor().getCaretModel().moveToLogicalPosition(new LogicalPosition(7, 6));
        assertEquals("Then the page shows \"Your quote\"", nextRequest(scenario, "highlight").body());
        waitFor(
                () ->
                        status.contains(
                                "axx: No text \"Your quote\" on the page; the page shows ..."));
    }

    public void testRunStepRunsTheStepWithItsTable() throws Exception {
        FakePausedScenario scenario = scenario();
        scenario.answer("POST", "run", 200, "passed");
        BlockingQueue<Notification> notifications = notifications();
        myFixture.configureByText("shop-portal.feature", feature(5));
        pause(scenario.url, null);

        myFixture.performEditorAction(RUN_STEP);
        assertEquals(
                "And the form is filled in with:\n| Weight | 2 kg |\n| Zone   | 3    |",
                nextRequest(scenario, "run").body());
        Notification passed = nextNotification(notifications);
        assertEquals("Passed: And the form is filled in with:", passed.getContent());

        scenario.answer("POST", "run", 422, "No field labeled \"Weight\" on the page");
        myFixture.performEditorAction(RUN_STEP);
        Notification failed = nextNotification(notifications);
        assertEquals("Failed: And the form is filled in with:", failed.getTitle());
        assertEquals("No field labeled &quot;Weight&quot; on the page", failed.getContent());
    }

    public void testInsertRecordedStepsFromTheRecordingFile() throws Exception {
        Path recording = FileUtil.createTempFile("recording", ".txt", true).toPath();
        Files.writeString(
                recording,
                "# Steps recorded in Playwright's Inspector\n"
                        + "  And the \"Get a quote\" button is clicked\n"
                        + "  # Not recorded: a hover over \"Zones\"\n",
                StandardCharsets.UTF_8);
        AxxPausedRuns.getInstance(getProject()).ended(pause(NOWHERE, recording));
        myFixture.configureByText("shop-portal.feature", feature(4));

        myFixture.performEditorAction(INSERT);
        myFixture.checkResult(
                String.join(
                        "\n",
                        "Feature: Shop portal",
                        "",
                        "  Scenario: Quote a parcel",
                        "    When the page \"/quote\" is opened",
                        "    And the \"Get a quote\" button is clicked",
                        "    # Not recorded: a hover over \"Zones\"<caret>",
                        "    And the form is filled in with:",
                        "      | Weight | 2 kg |",
                        "      | Zone   | 3    |",
                        "    Then the page shows \"Your quote\"",
                        ""));
    }

    public void testInsertRecordedStepsFromTheRunOffAStepLine() throws Exception {
        FakePausedScenario scenario = scenario();
        scenario.answer("GET", "recorded", 200, "      And the \"Get a quote\" button is clicked\n");
        pause(scenario.url, null);
        myFixture.configureByText("shop-portal.feature", feature(3));

        myFixture.performEditorAction(INSERT);
        myFixture.checkResult(
                String.join(
                        "\n",
                        "Feature: Shop portal",
                        "",
                        "  Scenario: Quote a parcel",
                        "      And the \"Get a quote\" button is clicked<caret>",
                        "    When the page \"/quote\" is opened",
                        "    And the form is filled in with:",
                        "      | Weight | 2 kg |",
                        "      | Zone   | 3    |",
                        "    Then the page shows \"Your quote\"",
                        ""));
    }

    /** The feature, with the caret at the start of a line (1-based). */
    private static String feature(int caretLine) {
        List<String> lines =
                new ArrayList<>(
                        List.of(
                                "Feature: Shop portal",
                                "",
                                "  Scenario: Quote a parcel",
                                "    When the page \"/quote\" is opened",
                                "    And the form is filled in with:",
                                "      | Weight | 2 kg |",
                                "      | Zone   | 3    |",
                                "    Then the page shows \"Your quote\"",
                                ""));
        lines.set(caretLine - 1, "<caret>" + lines.get(caretLine - 1));
        return String.join("\n", lines);
    }

    private Object pause(String url, Path recording) {
        Object run = new Object();
        runs.add(run);
        AxxPausedRuns.getInstance(getProject())
                .paused(run, new ScenarioPause(true, url, "features/shop-portal.feature:4", recording));
        return run;
    }

    private FakePausedScenario scenario() throws Exception {
        FakePausedScenario scenario = new FakePausedScenario();
        scenarios.add(scenario);
        return scenario;
    }

    private Presentation update(String id) {
        AnAction action = ActionManager.getInstance().getAction(id);
        AnActionEvent event = TestActionEvent.createTestEvent(action);
        ActionUtil.updateAction(action, event);
        return event.getPresentation();
    }

    private BlockingQueue<Notification> notifications() {
        BlockingQueue<Notification> notifications = new LinkedBlockingQueue<>();
        getProject()
                .getMessageBus()
                .connect(getTestRootDisposable())
                .subscribe(
                        Notifications.TOPIC,
                        new Notifications() {
                            @Override
                            public void notify(@NotNull Notification notification) {
                                notifications.add(notification);
                            }
                        });
        return notifications;
    }

    private static Notification nextNotification(BlockingQueue<Notification> notifications) {
        waitFor(() -> !notifications.isEmpty());
        return notifications.poll();
    }

    /** The next request to a path, skipping others (such as highlights). */
    private static Request nextRequest(FakePausedScenario scenario, String path) {
        List<Request> seen = new ArrayList<>();
        waitFor(
                () -> {
                    Request request;
                    while ((request = scenario.requests.poll()) != null) {
                        seen.add(request);
                        if (request.path().equals(path)) {
                            return true;
                        }
                    }
                    return false;
                });
        return seen.get(seen.size() - 1);
    }

    private static void waitFor(java.util.function.BooleanSupplier condition) {
        PlatformTestUtil.waitWithEventsDispatching("timed out", condition, 10);
    }
}
