// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.preview;

import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.Inlay;
import com.intellij.openapi.editor.event.EditorMouseEvent;
import com.intellij.openapi.editor.event.EditorMouseListener;
import com.intellij.openapi.editor.event.EditorMouseMotionListener;
import com.intellij.openapi.editor.ex.EditorEx;
import com.intellij.openapi.vfs.VirtualFile;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.awt.Cursor;
import java.awt.Rectangle;
import java.awt.event.MouseEvent;

/**
 * Clicks on the file previews and their toggles: a click on a toggle opens or closes its preview,
 * and a click on a preview opens the file, where edits happen. The pointer shows what is
 * clickable. The press is taken, so that the click does not move the caret.
 */
public final class FilePreviewMouse implements EditorMouseListener, EditorMouseMotionListener {
    /** What a click at the mouse would do. */
    private sealed interface Target permits Toggle, Preview {}

    private record Toggle(Editor editor, int line, VirtualFile file) implements Target {}

    private record Preview(FilePreview preview) implements Target {}

    /** What the press was on, to act on when the button is released on it. On the EDT only. */
    private @Nullable Target pressed;

    @Override
    public void mousePressed(@NotNull EditorMouseEvent event) {
        pressed = plainLeft(event) ? targetAt(event) : null;
        if (pressed != null) {
            event.consume();
        }
    }

    // The editor makes no click of a press a listener took, so the release acts.
    @Override
    public void mouseReleased(@NotNull EditorMouseEvent event) {
        Target target = pressed;
        pressed = null;
        if (target == null || !plainLeft(event) || !target.equals(targetAt(event))) {
            return;
        }
        event.consume();
        switch (target) {
            case Toggle t -> FilePreviews.toggle(t.editor(), t.line(), t.file());
            case Preview p -> p.preview().open();
        }
    }

    @Override
    public void mouseMoved(@NotNull EditorMouseEvent event) {
        if (event.getEditor() instanceof EditorEx editor) {
            editor.setCustomCursor(
                    FilePreviewMouse.class,
                    targetAt(event) != null ? Cursor.getPredefinedCursor(Cursor.HAND_CURSOR) : null);
        }
    }

    private static boolean plainLeft(@NotNull EditorMouseEvent event) {
        MouseEvent mouse = event.getMouseEvent();
        return mouse.getButton() == MouseEvent.BUTTON1
                && !mouse.isShiftDown()
                && !mouse.isControlDown()
                && !mouse.isMetaDown()
                && !mouse.isAltDown();
    }

    private static @Nullable Target targetAt(@NotNull EditorMouseEvent event) {
        Inlay<?> inlay = event.getInlay();
        if (inlay == null) {
            return null;
        }
        if (inlay.getRenderer() instanceof FilePreview preview) {
            return new Preview(preview);
        }
        if (inlay.getRenderer() instanceof FileToggle toggle) {
            Rectangle bounds = inlay.getBounds();
            if (bounds == null) {
                return null;
            }
            VirtualFile file = toggle.fileAt(inlay, event.getMouseEvent().getX() - bounds.x);
            Editor editor = event.getEditor();
            return file == null
                    ? null
                    : new Toggle(editor, editor.getDocument().getLineNumber(inlay.getOffset()), file);
        }
        return null;
    }
}
