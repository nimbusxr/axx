// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.lsp;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import org.junit.jupiter.params.provider.ValueSource;

@DisplayName("StepText")
class StepTextTest {

    @ParameterizedTest(name = "[{0}]")
    @DisplayName("is the text after the keyword up to the caret")
    @CsvSource(
            delimiter = '|',
            ignoreLeadingAndTrailingWhitespace = false,
            value = {
                "    Given the resp|the resp",
                "    When I send a GET request to \"/ord|I send a GET request to \"/ord",
                "    Then the response status code is 2|the response status code is 2",
                "    And the response |the response ",
                "    But no|no",
                "    * the service|the service",
                "\tGiven\tthe|the",
                "Given   the|the",
            })
    void typedText(String line, String typed) {
        assertEquals(typed, StepText.typedBefore(line));
    }

    @ParameterizedTest(name = "[{0}]")
    @DisplayName("is empty right after the keyword and its whitespace")
    @ValueSource(strings = {"    Given ", "    Then  ", "  * ", "And\t"})
    void emptyAfterKeyword(String line) {
        assertEquals("", StepText.typedBefore(line));
    }

    @ParameterizedTest(name = "[{0}]")
    @DisplayName("is null on lines that are not steps, or on the keyword itself")
    @ValueSource(
            strings = {
                "",
                "    ",
                "Feature: Orders",
                "  Scenario: Create an order",
                "    | name | value |",
                "    # Given a comment",
                "    Given",
                "    Giv",
                "    Givenness is",
                "    *the",
                "    given lower case",
            })
    void notAStep(String line) {
        assertNull(StepText.typedBefore(line));
    }

    @ParameterizedTest(name = "[{0}]")
    @DisplayName("is the table cell text up to the caret")
    @CsvSource(
            delimiter = '#',
            ignoreLeadingAndTrailingWhitespace = false,
            value = {
                "      | openapi | specs/par#specs/par",
                "      | openapi | #''",
                "      | seed |#''",
                "      |seeds/#seeds/",
                "      | a \\| b | seeds/m#seeds/m",
                "\t| url | file://.a#file://.a",
            })
    void cellText(String line, String typed) {
        assertEquals(typed, StepText.cellBefore(line));
    }

    @ParameterizedTest(name = "[{0}]")
    @DisplayName("has no cell text on lines that are not table rows")
    @ValueSource(strings = {"", "    ", "    Given a seeds/x.yaml db seed", "    # | a |"})
    void notARow(String line) {
        assertNull(StepText.cellBefore(line));
    }

    @Test
    @DisplayName("reads the line only up to the caret")
    void upToCaret() {
        String line = "    Given the response status code is 200";
        assertEquals("the resp", StepText.typedBefore(line.substring(0, 18)));
    }
}
