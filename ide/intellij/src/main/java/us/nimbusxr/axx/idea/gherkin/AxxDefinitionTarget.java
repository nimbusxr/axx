// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.openapi.editor.Document;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.fileEditor.OpenFileDescriptor;
import com.intellij.openapi.fileTypes.FileTypeManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.io.FileUtil;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.backend.navigation.NavigationRequest;
import com.intellij.psi.PsiElement;
import com.intellij.psi.PsiFile;
import com.intellij.psi.impl.FakePsiElement;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.net.URI;
import java.nio.file.Path;

import javax.swing.Icon;

/**
 * A place the axx language server named as a step's definition: a line and column in a file,
 * which may be a Go source file or a generated Markdown page.
 *
 * <p>The IDE may have no language support for the file (Go sources in IntelliJ IDEA without the Go
 * plugin are plain text, a single PSI element), so the target is the exact position, not the PSI
 * element there. A file the IDE has not seen yet, such as a page the server just generated, is
 * looked up again, refreshing, when the IDE navigates.
 */
final class AxxDefinitionTarget extends FakePsiElement {
    private final PsiFile source;
    private final Path path;
    private final @Nullable VirtualFile file;
    private final int line;
    private final int character;

    private AxxDefinitionTarget(
            PsiFile source, Path path, @Nullable VirtualFile file, int line, int character) {
        this.source = source;
        this.path = path;
        this.file = file;
        this.line = line;
        this.character = character;
    }

    /**
     * A target for a location, or null when the URI is not a local file.
     *
     * @param source the feature file navigated from
     * @param uri the location's {@code file:} URI
     * @param file the location's file, when the IDE knows it
     * @param line the zero-based line
     * @param character the zero-based UTF-16 column
     */
    static @Nullable AxxDefinitionTarget create(
            @NotNull PsiFile source,
            @NotNull String uri,
            @Nullable VirtualFile file,
            int line,
            int character) {
        Path path;
        try {
            path = file != null ? file.toNioPath() : Path.of(URI.create(uri));
        } catch (RuntimeException e) {
            // Not a file: URI, or not a local file.
            return null;
        }
        return new AxxDefinitionTarget(
                source, path, file, Math.max(0, line), Math.max(0, character));
    }

    /** The offset of the target position in the file's text, clamped to the text. */
    static int offset(@NotNull Document document, int line, int character) {
        if (document.getLineCount() == 0) {
            return 0;
        }
        int l = Math.min(line, document.getLineCount() - 1);
        int start = document.getLineStartOffset(l);
        return Math.min(start + character, document.getLineEndOffset(l));
    }

    @Override
    public @NotNull PsiElement getParent() {
        return source;
    }

    @Override
    public @NotNull Project getProject() {
        return source.getProject();
    }

    @Override
    public boolean isValid() {
        return source.isValid();
    }

    @Override
    public @NotNull String getName() {
        return path.getFileName() + ":" + (line + 1);
    }

    @Override
    public @NotNull String getPresentableText() {
        return getName();
    }

    @Override
    public @Nullable String getLocationString() {
        Path dir = path.getParent();
        return dir == null ? null : FileUtil.getLocationRelativeToUserHome(dir.toString());
    }

    @Override
    public @Nullable Icon getIcon(boolean open) {
        String name = path.getFileName().toString();
        return FileTypeManager.getInstance().getFileTypeByFileName(name).getIcon();
    }

    @Override
    public @NotNull PsiElement getNavigationElement() {
        return this;
    }

    @Override
    public boolean canNavigate() {
        return true;
    }

    @Override
    public boolean canNavigateToSource() {
        return true;
    }

    /** Where the IDE goes: the position, when the file is known; else see {@link #navigate}. */
    @Override
    public @Nullable NavigationRequest navigationRequest() {
        VirtualFile known =
                file != null && file.isValid()
                        ? file
                        : LocalFileSystem.getInstance().findFileByNioFile(path);
        Document document =
                known == null ? null : FileDocumentManager.getInstance().getDocument(known);
        if (document == null) {
            // Navigates with navigate(), on the UI thread, which may refresh.
            return super.navigationRequest();
        }
        return NavigationRequest.sourceNavigationRequest(
                getProject(), known, offset(document, line, character));
    }

    @Override
    public void navigate(boolean requestFocus) {
        VirtualFile found = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(path);
        if (found == null) {
            return;
        }
        Document document = FileDocumentManager.getInstance().getDocument(found);
        int offset = document == null ? 0 : offset(document, line, character);
        new OpenFileDescriptor(getProject(), found, offset).navigate(requestFocus);
    }
}
