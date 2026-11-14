// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.preview;

import com.intellij.openapi.editor.DefaultLanguageHighlighterColors;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.EditorCustomElementRenderer;
import com.intellij.openapi.editor.Inlay;
import com.intellij.openapi.editor.colors.EditorColorsScheme;
import com.intellij.openapi.editor.colors.EditorFontType;
import com.intellij.openapi.editor.ex.util.EditorUIUtil;
import com.intellij.openapi.editor.markup.TextAttributes;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.ui.JBColor;
import com.intellij.util.ui.JBUI;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.awt.Color;
import java.awt.Font;
import java.awt.FontMetrics;
import java.awt.Graphics2D;
import java.awt.geom.Rectangle2D;
import java.util.List;

/**
 * The toggles at the end of a line that names files: one for each file, which opens its preview
 * under the line ({@link FilePreview}) and closes it. Drawn as the IDE draws inlay hints, and
 * clicked with a plain click ({@link FilePreviewMouse}).
 */
final class FileToggle implements EditorCustomElementRenderer {
    private final Editor editor;
    private final List<VirtualFile> files;

    FileToggle(@NotNull Editor editor, @NotNull List<VirtualFile> files) {
        this.editor = editor;
        this.files = List.copyOf(files);
    }

    @NotNull
    List<VirtualFile> files() {
        return files;
    }

    /** A toggle's text: whether its preview is open, and the file when the line names several. */
    @NotNull
    String label(@NotNull Inlay<?> inlay, int index) {
        return (isOpen(inlay, index) ? "▾ " : "▸ ") + name(index);
    }

    private String name(int index) {
        return files.size() == 1 ? "preview" : files.get(index).getName();
    }

    /**
     * Whether a toggle's preview is open. The IDE measures an inlay before it places it, so its
     * offset may be past the document's end then.
     */
    private boolean isOpen(@NotNull Inlay<?> inlay, int index) {
        Document document = editor.getDocument();
        int offset = inlay.getOffset();
        return inlay.isValid()
                && offset >= 0
                && offset <= document.getTextLength()
                && FilePreviews.isOpen(editor, document.getLineNumber(offset), files.get(index));
    }

    private Font font() {
        Font font = editor.getColorsScheme().getFont(EditorFontType.PLAIN);
        return font.deriveFont(Math.max(font.getSize2D() - 1f, 8f));
    }

    private FontMetrics metrics() {
        return editor.getContentComponent().getFontMetrics(font());
    }

    private int gap() {
        return JBUI.scale(10);
    }

    private int padding() {
        return JBUI.scale(6);
    }

    private int between() {
        return JBUI.scale(4);
    }

    /** A toggle's width, the same open or closed. */
    private int width(int index) {
        FontMetrics metrics = metrics();
        int state = Math.max(metrics.stringWidth("▸ "), metrics.stringWidth("▾ "));
        return state + metrics.stringWidth(name(index)) + 2 * padding();
    }

    @Override
    public int calcWidthInPixels(@NotNull Inlay inlay) {
        int width = gap();
        for (int i = 0; i < files.size(); i++) {
            width += width(i) + (i > 0 ? between() : 0);
        }
        return width;
    }

    /** The file whose toggle is at x, from the inlay's left edge; null between toggles. */
    @Nullable
    VirtualFile fileAt(@NotNull Inlay<?> inlay, int x) {
        int at = gap();
        for (int i = 0; i < files.size(); i++) {
            int width = width(i);
            if (x >= at && x < at + width) {
                return files.get(i);
            }
            at += width + between();
        }
        return null;
    }

    @Override
    public void paint(
            @NotNull Inlay inlay,
            @NotNull Graphics2D g,
            @NotNull Rectangle2D region,
            @NotNull TextAttributes textAttributes) {
        EditorColorsScheme scheme = editor.getColorsScheme();
        TextAttributes hint = scheme.getAttributes(DefaultLanguageHighlighterColors.INLAY_DEFAULT);
        Color foreground =
                hint != null && hint.getForegroundColor() != null
                        ? hint.getForegroundColor()
                        : JBColor.GRAY;
        Color background =
                hint != null && hint.getBackgroundColor() != null
                        ? hint.getBackgroundColor()
                        : new JBColor(new Color(0, 0, 0, 20), new Color(255, 255, 255, 25));
        EditorUIUtil.setupAntialiasing(g);
        Font font = font();
        FontMetrics metrics = g.getFontMetrics(font);
        g.setFont(font);
        int height = Math.min((int) region.getHeight(), metrics.getHeight() + JBUI.scale(2));
        int top = (int) region.getY() + ((int) region.getHeight() - height) / 2;
        int baseline = top + (height - metrics.getHeight()) / 2 + metrics.getAscent();
        int x = (int) region.getX() + gap();
        int arc = JBUI.scale(8);
        for (int i = 0; i < files.size(); i++) {
            String label = label(inlay, i);
            int width = width(i);
            g.setColor(background);
            g.fillRoundRect(x, top, width, height, arc, arc);
            g.setColor(foreground);
            g.drawString(label, x + padding(), baseline);
            x += width + between();
        }
    }
}
