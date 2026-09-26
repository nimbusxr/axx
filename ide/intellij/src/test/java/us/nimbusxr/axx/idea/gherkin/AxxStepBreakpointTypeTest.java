// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VfsUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PsiTestUtil;
import com.intellij.xdebugger.XDebuggerUtil;

import us.nimbusxr.axx.idea.run.AxxStepBreakpoints;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/** Which lines of a feature file take a step breakpoint, with the Gherkin plugin. */
public class AxxStepBreakpointTypeTest extends HeavyPlatformTestCase {
    private static final String FEATURE =
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
                    "");

    private AxxStepBreakpointType type;
    private VirtualFile feature;
    private VirtualFile loose;

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        Path base = Path.of(getProject().getBasePath());
        Path suite = Files.createDirectories(base.resolve("acceptance/features")).getParent();
        Files.writeString(suite.resolve("axx.yaml"), "version: 1\n");
        Files.writeString(suite.resolve("features/shop-portal.feature"), FEATURE);
        Files.writeString(suite.resolve("features/notes.txt"), FEATURE);
        VirtualFile root = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(base);
        VfsUtil.markDirtyAndRefresh(false, true, true, root);
        PsiTestUtil.addContentRoot(getModule(), root);
        feature = root.findFileByRelativePath("acceptance/features/shop-portal.feature");
        loose = root.findFileByRelativePath("acceptance/features/notes.txt");
        type = XDebuggerUtil.getInstance().findBreakpointType(AxxStepBreakpointType.class);
    }

    public void testIsRegistered() {
        assertNotNull("the step breakpoint type is not registered", type);
        assertEquals(AxxStepBreakpoints.TYPE_ID, type.getId());
    }

    public void testOnlyStepLinesTakeABreakpoint() {
        assertEquals(List.of(6, 11, 12, 16, 17, 20, 21), linesTaking(feature));
    }

    public void testOnlyFeatureFiles() {
        assertEquals(List.of(), linesTaking(loose));
    }

    /** The one-based lines of a file that take a step breakpoint. */
    private List<Integer> linesTaking(VirtualFile file) {
        List<Integer> lines = new ArrayList<>();
        for (int line = 0; line < 30; line++) {
            if (type.canPutAt(file, line, getProject())) {
                lines.add(line + 1);
            }
        }
        return lines;
    }
}
