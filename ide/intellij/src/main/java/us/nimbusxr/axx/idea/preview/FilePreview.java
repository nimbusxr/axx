// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.preview;

import com.google.gson.GsonBuilder;
import com.google.gson.JsonParser;
import com.intellij.openapi.editor.DefaultLanguageHighlighterColors;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.EditorCustomElementRenderer;
import com.intellij.openapi.editor.Inlay;
import com.intellij.openapi.editor.RangeMarker;
import com.intellij.openapi.editor.colors.EditorColors;
import com.intellij.openapi.editor.colors.EditorColorsScheme;
import com.intellij.openapi.editor.colors.EditorFontType;
import com.intellij.openapi.editor.ex.util.EditorUIUtil;
import com.intellij.openapi.editor.highlighter.EditorHighlighter;
import com.intellij.openapi.editor.highlighter.EditorHighlighterFactory;
import com.intellij.openapi.editor.highlighter.HighlighterIterator;
import com.intellij.openapi.editor.markup.TextAttributes;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.fileEditor.OpenFileDescriptor;
import com.intellij.openapi.fileTypes.FileType;
import com.intellij.openapi.fileTypes.FileTypeManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.text.StringUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.ui.JBColor;
import com.intellij.util.ui.JBUI;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.awt.Color;
import java.awt.Font;
import java.awt.FontMetrics;
import java.awt.Graphics2D;
import java.awt.Image;
import java.awt.RenderingHints;
import java.awt.geom.Rectangle2D;
import java.awt.image.BufferedImage;
import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

import javax.imageio.ImageIO;

/**
 * The preview of a file a feature file names, under the line that names it: the first lines of a
 * text file, highlighted as the IDE highlights the file, or a picture's thumbnail. It only shows
 * the file; a click on it opens the file, where edits happen.
 */
final class FilePreview implements EditorCustomElementRenderer {
    /** How many lines of a text file it shows. */
    static final int MAX_LINES = 20;

    /** The characters of a line it shows. */
    static final int MAX_COLUMNS = 160;

    /** How much of a file it reads. */
    static final int MAX_BYTES = 1 << 20;

    /** The largest thumbnail, in the editor's pixels. */
    static final int THUMBNAIL_WIDTH = 360;

    static final int THUMBNAIL_HEIGHT = 200;

    /** The pictures it shows a thumbnail of. */
    private static final Set<String> PICTURES = Set.of("png", "jpg", "jpeg", "gif", "bmp");

    /** Extensions of files that are JSON, which the IDE may not know as JSON. */
    private static final Map<String, String> AS =
            Map.of("axxmodel", "json", "avsc", "json", "gltf", "json");

    /** What the preview shows. */
    sealed interface Content permits Text, Picture, Other {}

    /** A text file's first lines, as segments of one color each, and what it leaves out. */
    record Text(List<List<Segment>> lines, int more, boolean whole) implements Content {}

    record Segment(@NotNull String text, @Nullable Color color, int fontType) {}

    /** A picture's thumbnail, and the picture's size. */
    record Picture(@NotNull BufferedImage thumbnail, int width, int height) implements Content {}

    /** A file it cannot show, and why. */
    record Other(@NotNull String why) implements Content {}

    private final Project project;
    private final VirtualFile file;
    private final Editor editor;
    private final RangeMarker line;
    private @Nullable Content content;

    /**
     * The preview of a file under a line of the editor's document, which the marker keeps as the
     * document changes: the IDE measures an inlay before it places it, when its own offset is not
     * yet one of the document's.
     */
    FilePreview(
            @NotNull Project project,
            @NotNull VirtualFile file,
            @NotNull Editor editor,
            @NotNull RangeMarker line) {
        this.project = project;
        this.file = file;
        this.editor = editor;
        this.line = line;
    }

    @NotNull
    VirtualFile file() {
        return file;
    }

    /** Opens the file, to read it whole or to change it. */
    void open() {
        new OpenFileDescriptor(project, file).navigate(true);
    }

    /** Shows the file again as it is now: it changed. */
    void reload(@NotNull Inlay<?> inlay) {
        content = null;
        inlay.update();
        inlay.repaint();
    }

    @NotNull
    Content content() {
        if (content == null) {
            content = load(project, file, editor.getColorsScheme(), MAX_LINES);
        }
        return content;
    }

    // Layout: a box from the line's indentation, with a header line (the file's path and size),
    // then the lines, the thumbnail or why there is no preview, then what the lines leave out.

    private int padding() {
        return JBUI.scale(6);
    }

    private int margin() {
        return JBUI.scale(3);
    }

    private Font font(int fontType) {
        return editor.getColorsScheme().getFont(EditorFontType.forJavaStyle(fontType));
    }

    private FontMetrics metrics(int fontType) {
        return editor.getContentComponent().getFontMetrics(font(fontType));
    }

    /** The x of the line's text, where the box starts. */
    private int indent() {
        if (!line.isValid()) {
            return 0;
        }
        Document document = editor.getDocument();
        int number = document.getLineNumber(line.getStartOffset());
        CharSequence text = document.getCharsSequence();
        int at = document.getLineStartOffset(number);
        while (at < document.getLineEndOffset(number) && Character.isWhitespace(text.charAt(at))) {
            at++;
        }
        return editor.offsetToXY(at).x;
    }

    private int boxWidth() {
        FontMetrics plain = metrics(Font.PLAIN);
        int width = plain.stringWidth(header());
        switch (content()) {
            case Text text -> {
                for (List<Segment> line : text.lines()) {
                    int w = 0;
                    for (Segment s : line) {
                        w += metrics(s.fontType()).stringWidth(s.text());
                    }
                    width = Math.max(width, w);
                }
            }
            case Picture picture -> width = Math.max(width, shown(picture)[0]);
            case Other other -> width = Math.max(width, plain.stringWidth(other.why()));
        }
        return width + 2 * padding() + JBUI.scale(2);
    }

    @Override
    public int calcWidthInPixels(@NotNull Inlay inlay) {
        return indent() + boxWidth();
    }

    @Override
    public int calcHeightInPixels(@NotNull Inlay inlay) {
        int line = editor.getLineHeight();
        int body =
                switch (content()) {
                    case Text text ->
                            line * (text.lines().size() + (footer(text) == null ? 0 : 1));
                    case Picture picture -> shown(picture)[1] + padding();
                    case Other other -> line;
                };
        return 2 * margin() + 2 * padding() + line + body;
    }

    @Override
    public void paint(
            @NotNull Inlay inlay,
            @NotNull Graphics2D g,
            @NotNull Rectangle2D region,
            @NotNull TextAttributes textAttributes) {
        EditorColorsScheme scheme = editor.getColorsScheme();
        int x = (int) region.getX() + indent();
        int y = (int) region.getY() + margin();
        int width = boxWidth();
        int height = (int) region.getHeight() - 2 * margin();
        Color background = scheme.getColor(EditorColors.DOCUMENTATION_COLOR);
        g.setColor(background != null ? background : scheme.getDefaultBackground());
        g.fillRect(x, y, width, height);
        g.setColor(JBColor.border());
        g.fillRect(x, y, JBUI.scale(2), height);

        EditorUIUtil.setupAntialiasing(g);
        int left = x + JBUI.scale(2) + padding();
        int lineHeight = editor.getLineHeight();
        int ascent = editor.getAscent();
        int top = y + padding();
        Color muted = muted(scheme);
        g.setFont(font(Font.PLAIN));
        g.setColor(muted);
        g.drawString(header(), left, top + ascent);
        top += lineHeight;

        switch (content()) {
            case Text text -> {
                for (List<Segment> line : text.lines()) {
                    int at = left;
                    for (Segment s : line) {
                        g.setFont(font(s.fontType()));
                        g.setColor(s.color() != null ? s.color() : scheme.getDefaultForeground());
                        g.drawString(s.text(), at, top + ascent);
                        at += g.getFontMetrics().stringWidth(s.text());
                    }
                    top += lineHeight;
                }
                String footer = footer(text);
                if (footer != null) {
                    g.setFont(font(Font.PLAIN));
                    g.setColor(muted);
                    g.drawString(footer, left, top + ascent);
                }
            }
            case Picture picture -> {
                int[] size = shown(picture);
                g.setRenderingHint(
                        RenderingHints.KEY_INTERPOLATION, RenderingHints.VALUE_INTERPOLATION_BICUBIC);
                g.drawImage(picture.thumbnail(), left, top + padding() / 2, size[0], size[1], null);
            }
            case Other other -> {
                g.setColor(muted);
                g.drawString(other.why(), left, top + ascent);
            }
        }
    }

    private static Color muted(EditorColorsScheme scheme) {
        TextAttributes hint = scheme.getAttributes(DefaultLanguageHighlighterColors.INLAY_DEFAULT);
        return hint != null && hint.getForegroundColor() != null
                ? hint.getForegroundColor()
                : JBColor.GRAY;
    }

    /** The header: the file's path in the project, and its size. */
    @NotNull
    String header() {
        return header(project, file, content());
    }

    static @NotNull String header(
            @NotNull Project project, @NotNull VirtualFile file, @NotNull Content content) {
        String detail =
                switch (content) {
                    case Text text ->
                            text.whole()
                                    ? lines(text.lines().size() + text.more())
                                    : size(file.getLength());
                    case Picture picture -> picture.width() + " × " + picture.height();
                    case Other other -> size(file.getLength());
                };
        return path(project, file) + " · " + detail;
    }

    private static String path(@NotNull Project project, @NotNull VirtualFile file) {
        String base = project.getBasePath();
        if (base != null) {
            Path dir = Path.of(base);
            Path path = Path.of(file.getPath());
            if (path.startsWith(dir)) {
                return dir.relativize(path).toString().replace('\\', '/');
            }
        }
        return file.getPath();
    }

    static @Nullable String footer(@NotNull Text text) {
        if (!text.whole()) {
            return "… and the rest of the file";
        }
        return switch (text.more()) {
            case 0 -> null;
            case 1 -> "… and 1 more line";
            default -> "… and " + text.more() + " more lines";
        };
    }

    private static String lines(int n) {
        return n == 1 ? "1 line" : n + " lines";
    }

    /** A size for people: 418 bytes, 84 KB, 1.2 MB, as axx's hover gives it. */
    static String size(long n) {
        if (n == 1) {
            return "1 byte";
        }
        if (n < 1000) {
            return n + " bytes";
        }
        if (n < 1000 * 1000) {
            return (n + 500) / 1000 + " KB";
        }
        return String.format(Locale.ROOT, "%.1f MB", n / 1e6);
    }

    /** The size a picture's thumbnail is shown at, in the editor's pixels. */
    private static int[] shown(Picture picture) {
        return fit(picture, JBUI.scale(THUMBNAIL_WIDTH), JBUI.scale(THUMBNAIL_HEIGHT));
    }

    /** A picture's size, made to fit in a width and a height, never larger. */
    static int[] fit(Picture picture, int width, int height) {
        double scale =
                Math.min(
                        1.0,
                        Math.min(
                                (double) width / picture.width(),
                                (double) height / picture.height()));
        return new int[] {
            Math.max(1, (int) Math.round(picture.width() * scale)),
            Math.max(1, (int) Math.round(picture.height() * scale))
        };
    }

    /** What the preview shows of the file: of a text file, its first lines. */
    static @NotNull Content load(
            @NotNull Project project,
            @NotNull VirtualFile file,
            @NotNull EditorColorsScheme scheme,
            int maxLines) {
        if (!file.isValid()) {
            return new Other("The file is gone.");
        }
        String extension = StringUtil.notNullize(file.getExtension()).toLowerCase(Locale.ROOT);
        try {
            if (PICTURES.contains(extension)) {
                BufferedImage picture;
                try (InputStream in = file.getInputStream()) {
                    picture = ImageIO.read(in);
                }
                if (picture != null) {
                    return new Picture(
                            thumbnail(picture), picture.getWidth(), picture.getHeight());
                }
            }
            byte[] head;
            try (InputStream in = file.getInputStream()) {
                head = in.readNBytes(MAX_BYTES + 1);
            }
            boolean whole = head.length <= MAX_BYTES;
            String text = decode(head, whole);
            if (text == null) {
                return new Other("No preview: the file is neither text nor a picture.");
            }
            Document document = FileDocumentManager.getInstance().getCachedDocument(file);
            if (whole && document != null) {
                // With the changes not yet saved.
                text = document.getText();
            }
            text = text.replace("\r\n", "\n").replace("\t", "    ");
            String as = AS.get(extension);
            if (whole && ("json".equals(extension) || as != null)) {
                text = indented(text);
            }
            return text(project, file, as, text, whole, scheme, maxLines);
        } catch (IOException e) {
            return new Other("No preview: " + e.getMessage());
        }
    }

    /** The text of a file's head, or null when it is not text (UTF-8 without NUL bytes). */
    static @Nullable String decode(byte[] head, boolean whole) {
        int length = Math.min(head.length, MAX_BYTES);
        for (int i = 0; i < length; i++) {
            if (head[i] == 0) {
                return null;
            }
        }
        // The head of a larger file may end inside a character.
        for (int cut = 0; cut <= (whole ? 0 : 3) && cut <= length; cut++) {
            try {
                return StandardCharsets.UTF_8
                        .newDecoder()
                        .onMalformedInput(CodingErrorAction.REPORT)
                        .onUnmappableCharacter(CodingErrorAction.REPORT)
                        .decode(ByteBuffer.wrap(head, 0, length - cut))
                        .toString();
            } catch (CharacterCodingException e) {
                // Try a shorter head.
            }
        }
        return null;
    }

    /**
     * JSON with long lines, a stub's or a minified payload's, indented, so that its first lines
     * show its structure, as axx's hover shows it. Other JSON stays as written.
     */
    static @NotNull String indented(@NotNull String json) {
        boolean long_ = false;
        for (String line : json.split("\n", -1)) {
            if (line.length() > 200) {
                long_ = true;
                break;
            }
        }
        if (!long_) {
            return json;
        }
        try {
            return new GsonBuilder()
                    .setPrettyPrinting()
                    .disableHtmlEscaping()
                    .create()
                    .toJson(JsonParser.parseString(json));
        } catch (RuntimeException e) {
            return json;
        }
    }

    /** A text file's first lines, highlighted as the IDE highlights files of its type. */
    private static @NotNull Text text(
            @NotNull Project project,
            @NotNull VirtualFile file,
            @Nullable String as,
            @NotNull String text,
            boolean whole,
            @NotNull EditorColorsScheme scheme,
            int maxLines) {
        String trimmed = StringUtil.trimTrailing(text, '\n');
        String[] all = trimmed.split("\n", -1);
        int count = Math.min(all.length, maxLines);
        StringBuilder shown = new StringBuilder();
        for (int i = 0; i < count; i++) {
            String line = all[i];
            if (line.codePointCount(0, line.length()) > MAX_COLUMNS) {
                line = line.substring(0, line.offsetByCodePoints(0, MAX_COLUMNS)) + "…";
            }
            shown.append(line);
            if (i < count - 1) {
                shown.append('\n');
            }
        }

        EditorHighlighter highlighter;
        if (as != null) {
            FileType type = FileTypeManager.getInstance().getFileTypeByExtension(as);
            highlighter = EditorHighlighterFactory.getInstance().createEditorHighlighter(project, type);
        } else {
            highlighter = EditorHighlighterFactory.getInstance().createEditorHighlighter(project, file);
        }
        highlighter.setColorScheme(scheme);
        highlighter.setText(shown);

        List<List<Segment>> lines = new ArrayList<>();
        List<Segment> current = new ArrayList<>();
        HighlighterIterator it = highlighter.createIterator(0);
        while (!it.atEnd()) {
            String token = shown.substring(it.getStart(), it.getEnd());
            TextAttributes attributes = it.getTextAttributes();
            Color color = attributes != null ? attributes.getForegroundColor() : null;
            int fontType = attributes != null ? attributes.getFontType() : Font.PLAIN;
            String[] parts = token.split("\n", -1);
            for (int i = 0; i < parts.length; i++) {
                if (i > 0) {
                    lines.add(current);
                    current = new ArrayList<>();
                }
                if (!parts[i].isEmpty()) {
                    current.add(new Segment(parts[i], color, fontType));
                }
            }
            it.advance();
        }
        lines.add(current);
        while (lines.size() > count) {
            lines.removeLast();
        }
        return new Text(lines, whole ? all.length - count : 0, whole);
    }

    /**
     * A picture's thumbnail at twice the size it is shown at, at most, so that it is sharp on HiDPI
     * screens.
     */
    private static @NotNull BufferedImage thumbnail(@NotNull BufferedImage picture) {
        int width = picture.getWidth();
        int height = picture.getHeight();
        double scale =
                Math.min(
                        1.0,
                        Math.min(
                                2.0 * JBUI.scale(THUMBNAIL_WIDTH) / width,
                                2.0 * JBUI.scale(THUMBNAIL_HEIGHT) / height));
        if (scale >= 1.0) {
            return picture;
        }
        int w = Math.max(1, (int) Math.round(width * scale));
        int h = Math.max(1, (int) Math.round(height * scale));
        Image scaled = picture.getScaledInstance(w, h, Image.SCALE_AREA_AVERAGING);
        BufferedImage out = new BufferedImage(w, h, BufferedImage.TYPE_INT_ARGB);
        Graphics2D g = out.createGraphics();
        try {
            g.drawImage(scaled, 0, 0, null);
        } finally {
            g.dispose();
        }
        return out;
    }
}
