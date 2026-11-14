// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.preview;

import com.intellij.lang.documentation.DocumentationMarkup;
import com.intellij.model.Pointer;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.colors.EditorColorsManager;
import com.intellij.openapi.fileEditor.OpenFileDescriptor;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.text.StringUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.backend.documentation.DocumentationResult;
import com.intellij.platform.backend.documentation.DocumentationTarget;
import com.intellij.platform.backend.documentation.DocumentationTargetProvider;
import com.intellij.platform.backend.presentation.TargetPresentation;
import com.intellij.pom.Navigatable;
import com.intellij.psi.PsiDocumentManager;
import com.intellij.psi.PsiFile;
import com.intellij.ui.ColorUtil;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxProject;

import java.awt.Font;
import java.awt.Image;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;

/**
 * The documentation of a file's path in a feature file, which the IDE shows on hover and in Quick
 * Documentation: the file, as its preview shows it (its first lines, highlighted, or a picture's
 * thumbnail). axx's language server gives the same on hover, but the IDE puts the link's own
 * target, which has none, before the server's there; this one goes first.
 */
public final class FilePreviewDocumentation implements DocumentationTargetProvider {
    /** How many lines of a text file the documentation shows, as the server's hover does. */
    static final int MAX_LINES = 40;

    @Override
    public @NotNull List<? extends DocumentationTarget> documentationTargets(
            @NotNull PsiFile file, int offset) {
        VirtualFile feature = file.getVirtualFile();
        Project project = file.getProject();
        if (feature == null
                || !"feature".equals(feature.getExtension())
                || !AxxProject.isAxxProject(project)) {
            return List.of();
        }
        Document document = PsiDocumentManager.getInstance(project).getDocument(file);
        FileLinks.Links links = document == null ? null : FileLinks.of(project, feature, document);
        if (links == null) {
            return List.of();
        }
        for (FileLinks.Link link : links.links()) {
            if (link.range().isValid()
                    && link.range().getStartOffset() <= offset
                    && offset <= link.range().getEndOffset()) {
                return List.of(new Target(project, link.file()));
            }
        }
        return List.of();
    }

    /** A file a feature names. */
    record Target(@NotNull Project project, @NotNull VirtualFile file)
            implements DocumentationTarget {
        @Override
        public @NotNull Pointer<? extends DocumentationTarget> createPointer() {
            return () -> file.isValid() && !project.isDisposed() ? this : null;
        }

        @Override
        public @NotNull TargetPresentation computePresentation() {
            return TargetPresentation.builder(file.getName())
                    .icon(file.getFileType().getIcon())
                    .presentation();
        }

        @Override
        public @Nullable Navigatable getNavigatable() {
            return new OpenFileDescriptor(project, file);
        }

        @Override
        public @Nullable DocumentationResult computeDocumentation() {
            FilePreview.Content content =
                    FilePreview.load(
                            project,
                            file,
                            EditorColorsManager.getInstance().getGlobalScheme(),
                            MAX_LINES);
            Html html = html(project, file, content);
            return DocumentationResult.Companion.documentation(html.html()).images(html.images());
        }
    }

    /** A documentation's HTML, and the images it shows, by their addresses in it. */
    record Html(@NotNull String html, @NotNull Map<String, Image> images) {}

    /** The file's documentation: its path and size, then what its preview shows. */
    static @NotNull Html html(
            @NotNull Project project,
            @NotNull VirtualFile file,
            @NotNull FilePreview.Content content) {
        StringBuilder b = new StringBuilder();
        b.append(DocumentationMarkup.DEFINITION_START)
                .append(StringUtil.escapeXmlEntities(FilePreview.header(project, file, content)))
                .append(DocumentationMarkup.DEFINITION_END)
                .append(DocumentationMarkup.CONTENT_START);
        Map<String, Image> images = Map.of();
        switch (content) {
            case FilePreview.Text text -> {
                b.append("<pre><code>");
                for (int i = 0; i < text.lines().size(); i++) {
                    if (i > 0) {
                        b.append('\n');
                    }
                    for (FilePreview.Segment s : text.lines().get(i)) {
                        String escaped = StringUtil.escapeXmlEntities(s.text());
                        if ((s.fontType() & Font.BOLD) != 0) {
                            escaped = "<b>" + escaped + "</b>";
                        }
                        if ((s.fontType() & Font.ITALIC) != 0) {
                            escaped = "<i>" + escaped + "</i>";
                        }
                        b.append(
                                s.color() == null
                                        ? escaped
                                        : "<span style=\"color:"
                                                + ColorUtil.toHtmlColor(s.color())
                                                + "\">"
                                                + escaped
                                                + "</span>");
                    }
                }
                b.append("</code></pre>");
                String footer = FilePreview.footer(text);
                if (footer != null) {
                    b.append("<p>").append(StringUtil.escapeXmlEntities(footer)).append("</p>");
                }
            }
            case FilePreview.Picture picture -> {
                int[] size =
                        FilePreview.fit(
                                picture, FilePreview.THUMBNAIL_WIDTH, FilePreview.THUMBNAIL_HEIGHT);
                // The picture's own address, which the IDE's HTML resolves from the images
                // given with the documentation where it can, and loads from the file elsewhere (in
                // the hover's popup), shown at the thumbnail's size.
                String src = Path.of(file.getPath()).toUri().toString();
                b.append("<img src=\"")
                        .append(StringUtil.escapeXmlEntities(src))
                        .append("\" width=\"")
                        .append(size[0])
                        .append("\" height=\"")
                        .append(size[1])
                        .append("\">");
                images = Map.of(src, picture.thumbnail());
            }
            case FilePreview.Other other ->
                    b.append("<p>").append(StringUtil.escapeXmlEntities(other.why())).append("</p>");
        }
        b.append(DocumentationMarkup.CONTENT_END);
        return new Html(b.toString(), images);
    }
}
