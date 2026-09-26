// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.List;

@DisplayName("FeatureSteps")
class FeatureStepsTest {
    // The feature of AxxStepBreakpointTypeTest, where the Gherkin plugin finds the same steps.
    private static final List<String> FEATURE =
            FeatureSteps.lines(
                    String.join(
                            "\n",
                            "@web", // 1
                            "Feature: Shop portal",
                            "  Given in a description, not a step", // 3
                            "",
                            "  Background:", // 5
                            "    Given the shop portal with the following properties:",
                            "      | url | http://localhost:8400 |", // 7
                            "",
                            "  Scenario: Register a parcel", // 9
                            "    # When a comment",
                            "    When the page \"/parcels/new\" is opened", // 11
                            "    And the text \"Register\" is entered in:",
                            "      \"\"\"", // 13
                            "      Then not a step",
                            "      \"\"\"", // 15
                            "    * the button \"Register\" is clicked",
                            "    Then the page shows \"Registered\"", // 17
                            "",
                            "  Scenario Outline: Track <parcel>", // 19
                            "    When the page \"/track/<parcel>\" is opened",
                            "    But the page does not show \"Unknown parcel\"", // 21
                            "",
                            "    Examples:", // 23
                            "      | parcel |",
                            "      | P-1    |", // 25
                            ""));

    @Test
    @DisplayName("finds the steps' lines, as the Gherkin plugin does")
    void stepLines() {
        List<Integer> lines = new ArrayList<>();
        for (int line = -1; line < FEATURE.size() + 2; line++) {
            if (FeatureSteps.stepAt(FEATURE, line) != null) {
                lines.add(line + 1);
            }
        }
        assertEquals(List.of(6, 11, 12, 16, 17, 20, 21), lines);
    }

    @Test
    @DisplayName("gives a step's text, trimmed, with its keyword")
    void stepText() {
        assertEquals("* the button \"Register\" is clicked", FeatureSteps.stepAt(FEATURE, 15));
        assertEquals(
                "Then the shop shows \"Registered\"",
                FeatureSteps.stepAt(
                        FeatureSteps.lines("Feature: A\r\nScenario: B\r\n\tThen the shop shows \"Registered\"  \r\n"),
                        2));
    }

    @Test
    @DisplayName("gives a step with the rows of its table right below it")
    void stepWithTable() {
        assertEquals(
                "Given the shop portal with the following properties:\n| url | http://localhost:8400 |",
                FeatureSteps.stepWithTable(FEATURE, 5));
        assertEquals("When the page \"/parcels/new\" is opened", FeatureSteps.stepWithTable(FEATURE, 10));
        // A doc string is not a table.
        assertEquals("And the text \"Register\" is entered in:", FeatureSteps.stepWithTable(FEATURE, 11));
        assertNull(FeatureSteps.stepWithTable(FEATURE, 6));
        List<String> lines =
                FeatureSteps.lines(
                        String.join(
                                "\n",
                                "Feature: Quotes",
                                "  Scenario: Quote a parcel",
                                "    When the form is filled in with:",
                                "      | Weight | 2 kg |",
                                "      | Zone   | 3    |",
                                "    # the quote",
                                "      | not | this |"));
        assertEquals(
                "When the form is filled in with:\n| Weight | 2 kg |\n| Zone   | 3    |",
                FeatureSteps.stepWithTable(lines, 2));
    }

    @Test
    @DisplayName("re-indents recorded steps, keeping their comments and relative indentation")
    void reindent() {
        String recorded =
                "\n  When the page \"/quote\" is opened  \n"
                        + "  # Not recorded: a drag and drop\n"
                        + "\n"
                        + "  And the \"Get a quote\" button is clicked\n"
                        + "    | a | b |\n"
                        + "\n\n";
        assertEquals(
                "      When the page \"/quote\" is opened\n"
                        + "      # Not recorded: a drag and drop\n"
                        + "\n"
                        + "      And the \"Get a quote\" button is clicked\n"
                        + "        | a | b |",
                FeatureSteps.reindent(recorded, "      "));
        assertEquals(
                "  When the page \"/quote\" is opened\n"
                        + "  # Not recorded: a drag and drop\n"
                        + "\n"
                        + "  And the \"Get a quote\" button is clicked\n"
                        + "    | a | b |",
                FeatureSteps.reindent(recorded, null));
        assertEquals("\tThen done", FeatureSteps.reindent("        Then done", "\t"));
        assertEquals("", FeatureSteps.reindent("\n  \n", "    "));
    }

    @Test
    @DisplayName("gives the indentation of a line")
    void indentOf() {
        assertEquals("    ", FeatureSteps.indentOf("    When x"));
        assertEquals("\t ", FeatureSteps.indentOf("\t When x"));
        assertEquals("", FeatureSteps.indentOf("When x"));
    }

    @Test
    @DisplayName("drops the comment line a recording file starts with")
    void recordingFile() {
        assertEquals(
                "    When the page \"/quote\" is opened\n    # Not recorded: a hover\n",
                FeatureSteps.fromRecordingFile(
                        "# Steps recorded in Playwright's Inspector\n"
                                + "    When the page \"/quote\" is opened\n"
                                + "    # Not recorded: a hover\n"));
        assertEquals("    When x\n", FeatureSteps.fromRecordingFile("    When x\n"));
        assertEquals("", FeatureSteps.fromRecordingFile("# Steps recorded in Playwright's Inspector"));
        assertEquals("", FeatureSteps.fromRecordingFile(""));
    }
}
