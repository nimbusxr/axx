// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.preview;

import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.EditorFactory;
import com.intellij.openapi.editor.Inlay;
import com.intellij.openapi.editor.colors.EditorColorsManager;
import com.intellij.openapi.editor.colors.EditorColorsScheme;
import com.intellij.openapi.editor.markup.TextAttributes;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VfsUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PlatformTestUtil;

import java.awt.Color;
import java.awt.Graphics2D;
import java.awt.Rectangle;
import java.awt.geom.Rectangle2D;
import java.awt.image.BufferedImage;
import java.io.ByteArrayOutputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

import javax.imageio.ImageIO;

/** What a file's preview shows, and how it draws it. */
public class FilePreviewTest extends HeavyPlatformTestCase {
    private Path base;

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        base = Path.of(getProject().getBasePath());
    }

    private VirtualFile write(String name, byte[] content) throws Exception {
        Path path = base.resolve(name);
        Files.createDirectories(path.getParent());
        Files.write(path, content);
        VirtualFile file = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(path);
        VfsUtil.markDirtyAndRefresh(false, false, false, file);
        return file;
    }

    private VirtualFile write(String name, String content) throws Exception {
        return write(name, content.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    }

    private FilePreview.Content load(VirtualFile file) {
        EditorColorsScheme scheme = EditorColorsManager.getInstance().getGlobalScheme();
        return FilePreview.load(getProject(), file, scheme, FilePreview.MAX_LINES);
    }

    private static List<String> lines(FilePreview.Text text) {
        List<String> out = new ArrayList<>();
        for (List<FilePreview.Segment> line : text.lines()) {
            StringBuilder b = new StringBuilder();
            line.forEach(s -> b.append(s.text()));
            out.add(b.toString());
        }
        return out;
    }

    private static byte[] png(int width, int height) throws Exception {
        BufferedImage image = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
        Graphics2D g = image.createGraphics();
        g.setColor(new Color(0x2f, 0x6f, 0xb3));
        g.fillRect(0, 0, width, height);
        g.setColor(Color.WHITE);
        g.fillRect(width / 4, height / 4, width / 2, height / 2);
        g.dispose();
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        ImageIO.write(image, "png", out);
        return out.toByteArray();
    }

    public void testATextFile() throws Exception {
        VirtualFile seed =
                write(
                        "features/seeds/manifest-kestrel.yaml",
                        "manifest_lines:\n  - id: ML-KES-0412-1\n    status: NEW\n");
        FilePreview.Text text = (FilePreview.Text) load(seed);
        assertEquals(
                List.of("manifest_lines:", "  - id: ML-KES-0412-1", "    status: NEW"), lines(text));
        assertEquals(0, text.more());
        assertNull(FilePreview.footer(text));
        // Highlighted as the IDE highlights YAML: the key and the value in colors of their own.
        List<FilePreview.Segment> first = text.lines().get(0);
        assertEquals("manifest_lines", first.get(0).text());
        assertNotNull(first.get(0).color());
    }

    public void testALongFilesFirstLines() throws Exception {
        StringBuilder b = new StringBuilder();
        for (int i = 1; i <= 50; i++) {
            b.append("- parcel: PX-").append(i).append('\n');
        }
        FilePreview.Text text = (FilePreview.Text) load(write("seeds/long.yaml", b.toString()));
        assertEquals(FilePreview.MAX_LINES, text.lines().size());
        assertEquals("- parcel: PX-20", lines(text).get(19));
        assertEquals("… and 30 more lines", FilePreview.footer(text));
    }

    public void testJsonOnOneLongLineIsIndented() throws Exception {
        String stub =
                "{\"version\":1,\"api\":\"android.mlkit.text-recognition\",\"note\":\""
                        + "x".repeat(300)
                        + "\"}";
        FilePreview.Text text = (FilePreview.Text) load(write("models/torn-label.axxmodel", stub));
        List<String> lines = lines(text);
        assertEquals("{", lines.get(0));
        assertEquals("  \"version\": 1,", lines.get(1));
        assertEquals("  \"api\": \"android.mlkit.text-recognition\",", lines.get(2));
        // A long line is cut.
        assertEquals(FilePreview.MAX_COLUMNS + 1, lines.get(3).length());
        assertTrue(lines.get(3).endsWith("…"));
    }

    public void testUnsavedChanges() throws Exception {
        VirtualFile seed = write("seeds/parcels.yaml", "parcels: []\n");
        var document = FileDocumentManager.getInstance().getDocument(seed);
        com.intellij.openapi.command.WriteCommandAction.runWriteCommandAction(
                getProject(), () -> document.setText("parcels:\n  - PX-1\n"));
        assertEquals(List.of("parcels:", "  - PX-1"), lines((FilePreview.Text) load(seed)));
    }

    public void testAPicture() throws Exception {
        FilePreview.Picture picture =
                (FilePreview.Picture) load(write("labels/label.png", png(1600, 1200)));
        assertEquals(1600, picture.width());
        assertEquals(1200, picture.height());
        // At most twice the size it is shown at.
        assertTrue(picture.thumbnail().getHeight() <= 2 * FilePreview.THUMBNAIL_HEIGHT);
        assertEquals(
                1600.0 / 1200,
                (double) picture.thumbnail().getWidth() / picture.thumbnail().getHeight(),
                0.01);
    }

    /** A picture's documentation shows it at its thumbnail's size, from the file's address. */
    public void testAPicturesDocumentation() throws Exception {
        VirtualFile label = write("labels/label.png", png(1080, 1920));
        FilePreviewDocumentation.Html html =
                FilePreviewDocumentation.html(getProject(), label, load(label));
        String src = Path.of(label.getPath()).toUri().toString();
        assertTrue(html.html(), html.html().contains("labels/label.png · 1080 × 1920"));
        assertTrue(
                html.html(),
                html.html().contains("<img src=\"" + src + "\" width=\"113\" height=\"200\">"));
        assertEquals(java.util.Set.of(src), html.images().keySet());
    }

    public void testAFileThatIsNeither() throws Exception {
        FilePreview.Content content =
                load(write("labels/lifted.mp4", new byte[] {0, 0, 0, 0x18, 'f', 't', 'y', 'p'}));
        assertEquals(
                new FilePreview.Other("No preview: the file is neither text nor a picture."),
                content);
    }

    public void testSizes() {
        assertEquals("1 byte", FilePreview.size(1));
        assertEquals("418 bytes", FilePreview.size(418));
        assertEquals("84 KB", FilePreview.size(84_300));
        assertEquals("1.2 MB", FilePreview.size(1_234_567));
    }

    /** The preview under a line of a feature, drawn as the editor draws it. */
    public void testDrawn() throws Exception {
        VirtualFile seed =
                write(
                        "features/seeds/manifest-kestrel.yaml",
                        "manifest_lines:\n  - id: ML-KES-0412-1\n    status: NEW\n");
        VirtualFile label = write("features/labels/label.png", png(800, 600));
        VirtualFile feature =
                write(
                        "features/manifest.feature",
                        "Feature: Manifests\n\n  Scenario: imported\n"
                                + "    Given a seeds/manifest-kestrel.yaml db seed\n"
                                + "    And the courier app's camera sees labels/label.png\n");
        var document = FileDocumentManager.getInstance().getDocument(feature);
        Editor editor = EditorFactory.getInstance().createEditor(document, getProject(), feature, false);
        try {
            FilePreviews.toggle(editor, 3, seed);
            FilePreviews.toggle(editor, 4, label);
            assertTrue(FilePreviews.isOpen(editor, 3, seed));
            assertFalse(FilePreviews.isOpen(editor, 3, label));
            List<Inlay<FilePreview>> open = FilePreviews.all(editor);
            assertEquals(2, open.size());
            assertEquals(
                    "features/seeds/manifest-kestrel.yaml · 3 lines",
                    open.get(0).getRenderer().header());
            assertEquals("features/labels/label.png · 800 × 600", open.get(1).getRenderer().header());
            for (Inlay<FilePreview> inlay : open) {
                assertNotBlank(draw(inlay));
            }

            // Clicking the toggle again closes it.
            FilePreviews.toggle(editor, 3, seed);
            assertFalse(FilePreviews.isOpen(editor, 3, seed));
            assertEquals(1, FilePreviews.all(editor).size());

            // The preview follows the file.
            com.intellij.openapi.command.WriteCommandAction.runWriteCommandAction(
                    getProject(),
                    () ->
                            FileDocumentManager.getInstance()
                                    .getDocument(seed)
                                    .setText("manifest_lines: []\n"));
            FilePreviews.toggle(editor, 3, seed);
            Inlay<FilePreview> reopened = FilePreviews.all(editor).get(1);
            assertEquals("features/seeds/manifest-kestrel.yaml · 1 line", reopened.getRenderer().header());
            com.intellij.openapi.command.WriteCommandAction.runWriteCommandAction(
                    getProject(),
                    () ->
                            FileDocumentManager.getInstance()
                                    .getDocument(seed)
                                    .setText("manifest_lines:\n  - id: ML-KES-0412-2\n"));
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            assertEquals("features/seeds/manifest-kestrel.yaml · 2 lines", reopened.getRenderer().header());
        } finally {
            EditorFactory.getInstance().releaseEditor(editor);
        }
    }

    /** Asserts that something other than the background was drawn. */
    private static void assertNotBlank(BufferedImage image) {
        int background = image.getRGB(0, 0);
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                if (image.getRGB(x, y) != background) {
                    return;
                }
            }
        }
        fail("nothing was drawn");
    }

    /** Draws an inlay into an image of its size, on the editor's background. */
    private static BufferedImage draw(Inlay<FilePreview> inlay) {
        int width = inlay.getRenderer().calcWidthInPixels(inlay);
        int height = inlay.getRenderer().calcHeightInPixels(inlay);
        assertTrue(width > 0 && height > 0);
        BufferedImage image = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
        Graphics2D g = image.createGraphics();
        try {
            g.setColor(inlay.getEditor().getColorsScheme().getDefaultBackground());
            g.fillRect(0, 0, width, height);
            inlay.getRenderer()
                    .paint(inlay, g, (Rectangle2D) new Rectangle(0, 0, width, height), new TextAttributes());
        } finally {
            g.dispose();
        }
        return image;
    }
}
