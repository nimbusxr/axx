// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.lsp;

import com.intellij.execution.ExecutionException;
import com.intellij.execution.configurations.GeneralCommandLine;
import com.intellij.openapi.editor.colors.TextAttributesKey;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.lsp.api.ProjectWideLspServerDescriptor;
import com.intellij.platform.lsp.api.customization.LspCompletionCustomizer;
import com.intellij.platform.lsp.api.customization.LspCustomization;
import com.intellij.platform.lsp.api.customization.LspSemanticTokensCustomizer;
import com.intellij.platform.lsp.api.customization.LspSemanticTokensSupport;
import com.intellij.psi.PsiFile;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxBinary;

import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.List;

/**
 * How to run the axx language server: {@code axx lsp}, over stdio, in the project directory. The
 * server finds the axx project from there itself.
 */
final class AxxLspServerDescriptor extends ProjectWideLspServerDescriptor {
    private final LspCustomization customization = new AxxLspCustomization();

    AxxLspServerDescriptor(@NotNull Project project) {
        super(project, "axx");
    }

    /** Whether the server serves a file: Gherkin feature files. */
    static boolean isFeatureFile(@NotNull VirtualFile file) {
        return !file.isDirectory() && "feature".equals(file.getExtension());
    }

    @Override
    public boolean isSupportedFile(@NotNull VirtualFile file) {
        return isFeatureFile(file);
    }

    @Override
    public @NotNull GeneralCommandLine createCommandLine() throws ExecutionException {
        String basePath = getProject().getBasePath();
        if (basePath == null) {
            throw new ExecutionException(
                    "Cannot start the axx language server: the project has no directory.");
        }
        Path projectDir = Path.of(basePath);
        Path executable = AxxBinary.find(projectDir, "start the axx language server");
        return new GeneralCommandLine(executable.toString(), "lsp")
                .withWorkingDirectory(projectDir)
                .withCharset(StandardCharsets.UTF_8);
    }

    @Override
    public @NotNull LspCustomization getLspCustomization() {
        return customization;
    }

    /**
     * The colors of the server's semantic tokens: the Gherkin plugin's, looked up by name since the
     * plugin is optional. Step parameter values get the Gherkin <i>Step parameter</i> color (bold
     * blue by default, as Cucumber's step parameters), and {@code <placeholders>} the Gherkin
     * plugin's own placeholder color. The IDE's generic Parameter color is plain text in the
     * bundled schemes.
     */
    static @Nullable TextAttributesKey tokenColor(@NotNull String tokenType) {
        return switch (tokenType) {
            case "parameter" -> TextAttributesKey.find("GHERKIN_REGEXP_PARAMETER");
            case "variable" -> TextAttributesKey.find("GHERKIN_OUTLINE_PARAMETER_SUBSTITUTION");
            default -> null;
        };
    }

    /**
     * The default features, with semantic tokens asked for in every feature file and colored as
     * Gherkin colors step parameters, and completion matched against the whole step text.
     */
    private static final class AxxLspCustomization extends LspCustomization {
        private final LspSemanticTokensCustomizer semanticTokens =
                new LspSemanticTokensSupport() {
                    // By default the IDE asks only for files it has no language support for, and
                    // the Gherkin plugin makes feature files a language.
                    @Override
                    public boolean shouldAskServerForSemanticTokens(@NotNull PsiFile psiFile) {
                        return true;
                    }

                    @Override
                    public @Nullable TextAttributesKey getTextAttributesKey(
                            @NotNull String tokenType, @NotNull List<String> modifiers) {
                        TextAttributesKey color = tokenColor(tokenType);
                        return color != null
                                ? color
                                : super.getTextAttributesKey(tokenType, modifiers);
                    }
                };

        private final LspCompletionCustomizer completion = new AxxCompletionSupport();

        @Override
        public @NotNull LspSemanticTokensCustomizer getSemanticTokensCustomizer() {
            return semanticTokens;
        }

        @Override
        public @NotNull LspCompletionCustomizer getCompletionCustomizer() {
            return completion;
        }
    }
}
