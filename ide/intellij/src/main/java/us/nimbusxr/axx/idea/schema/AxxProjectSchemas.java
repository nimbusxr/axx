// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import com.google.gson.JsonParser;
import com.intellij.execution.ExecutionException;
import com.intellij.execution.configurations.GeneralCommandLine;
import com.intellij.execution.process.ProcessOutput;
import com.intellij.execution.util.ExecUtil;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.application.PathManager;
import com.intellij.openapi.components.Service;
import com.intellij.openapi.diagnostic.Logger;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.io.NioFiles;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VirtualFile;
import com.jetbrains.jsonSchema.ide.JsonSchemaService;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxBinary;
import us.nimbusxr.axx.idea.ConfigFinder;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.attribute.BasicFileAttributes;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;
import java.util.Objects;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.stream.Stream;

/**
 * The schemas of the axx a project runs on, as {@code axx schema} prints them. The IDE completes
 * and checks axx.yaml and the fixture spec files with these, so they match the axx that reads the
 * files, whichever release of the plugin is installed. The axx a project runs on is the one the
 * plugin finds ({@link AxxBinary}), run in the folder of the project's axx.yaml: it hands over to
 * the version that axx.yaml names, when it names one.
 *
 * <p>The schemas are read again when that axx may have changed: when the project opens, when the
 * axx executable setting changes, when an axx.yaml changes, and when the IDE comes back to the
 * front (at most every 30 seconds). Each time, {@code axx --version} says which axx it is, and
 * only a different one is asked for its schemas. Until axx has given them, and when it can't, the
 * IDE uses the schemas the plugin bundles.
 */
@Service(Service.Level.PROJECT)
public final class AxxProjectSchemas {
    private static final Logger LOG = Logger.getInstance(AxxProjectSchemas.class);

    /** How long axx may take to answer: asked first for a version a project names, it downloads it. */
    private static final int TIMEOUT_MS = 120_000;

    /** How often, at most, the IDE coming back to the front checks whether axx changed. */
    private static final long FRONT_CHECK_MS = 30_000;

    /** The address the schemas' $id has, and the docs site's, which earlier axx.yaml files name. */
    static final String GITHUB = "https://raw.githubusercontent.com/nimbusxr/axx/main/schemas/v0/";

    static final String SITE = "https://axx.nimbusxr.us/schemas/v0/";

    private final Project project;
    private final AtomicBoolean busy = new AtomicBoolean();
    private volatile boolean again;
    private volatile long lastFrontCheck;

    /** The schemas axx gave: which axx, and the folder they are in. Null for the bundled ones. */
    private volatile @Nullable Given given;

    /** The axx asked last, whether it gave its schemas or not: it is not asked again. */
    private volatile @Nullable String asked;

    /** Schemas an axx gave. */
    record Given(@NotNull String axx, @NotNull Path dir) {}

    public AxxProjectSchemas(@NotNull Project project) {
        this.project = project;
    }

    public static @NotNull AxxProjectSchemas getInstance(@NotNull Project project) {
        return project.getService(AxxProjectSchemas.class);
    }

    /**
     * The file of a schema the project's axx gave.
     *
     * @param site the copy whose $id is the docs site's address
     * @return the file, or null while the plugin's own is in use
     */
    @Nullable VirtualFile file(@NotNull AxxSchemas.Schema schema, boolean site) {
        Given g = given;
        if (g == null) {
            return null;
        }
        return LocalFileSystem.getInstance().findFileByNioFile(path(g.dir(), schema, site));
    }

    /** Reads the schemas again, in the background, if the axx the project runs on changed. */
    public void refresh() {
        String base = project.getBasePath();
        if (base == null || project.isDisposed() || project.isDefault()) {
            return;
        }
        if (!busy.compareAndSet(false, true)) {
            again = true;
            return;
        }
        ApplicationManager.getApplication()
                .executeOnPooledThread(
                        () -> {
                            try {
                                do {
                                    again = false;
                                    update(Path.of(base));
                                } while (again && !project.isDisposed());
                            } finally {
                                busy.set(false);
                            }
                        });
    }

    /** As {@link #refresh}, when the IDE comes back to the front: at most every 30 seconds. */
    void refreshOnFront() {
        long now = System.currentTimeMillis();
        if (now - lastFrontCheck < FRONT_CHECK_MS) {
            return;
        }
        lastFrontCheck = now;
        refresh();
    }

    /**
     * Asks the axx a project in base runs on for its schemas, if it isn't the one asked last. Not
     * on the event dispatch thread: it runs axx.
     */
    void update(@NotNull Path base) {
        Path config = ConfigFinder.find(base);
        if (config == null || project.isDisposed()) {
            return;
        }
        Path dir = config.getParent();
        Path axx;
        try {
            axx = AxxBinary.find(dir, "read axx's schemas");
        } catch (ExecutionException e) {
            asked = null;
            use(null);
            return;
        }
        String which = identify(axx, dir);
        if (which == null) {
            asked = null;
            use(null);
            return;
        }
        if (which.equals(asked)) {
            return;
        }
        asked = which;
        Path out = cacheDir().resolve(sha256(which));
        if (!Files.isDirectory(out) && !readAll(axx, dir, out)) {
            use(null);
            return;
        }
        use(new Given(which, out));
        removeOthers(out);
    }

    /**
     * Which axx this is: its {@code --version} (with the version an axx.yaml names, the one it hands
     * over to), and its file as it is now, for builds of one version.
     */
    private static @Nullable String identify(Path axx, Path dir) {
        ProcessOutput out = run(List.of(axx.toString(), "--version"), dir);
        if (out == null) {
            return null;
        }
        String file = axx.toString();
        try {
            Path real = axx.toRealPath();
            BasicFileAttributes attrs = Files.readAttributes(real, BasicFileAttributes.class);
            file = real + " " + attrs.size() + " " + attrs.lastModifiedTime().toMillis();
        } catch (IOException ignored) {
            // the version alone, then
        }
        return out.getStdout().strip() + "\n" + file;
    }

    /** Has axx write every schema into out; false, with nothing in out, when it can't. */
    private boolean readAll(Path axx, Path dir, Path out) {
        Path tmp = null;
        try {
            Files.createDirectories(out.getParent());
            tmp = Files.createTempDirectory(out.getParent(), ".reading-");
            Files.createDirectories(tmp.resolve("site"));
            for (AxxSchemas.Schema schema : AxxSchemas.ALL) {
                Path file = path(tmp, schema, false);
                List<String> command =
                        List.of(axx.toString(), "schema", "--kind", schema.kind(), "--out", file.toString());
                if (run(command, dir) == null || !isSchema(file)) {
                    LOG.info("axx at " + axx + " gave no " + schema.kind() + " schema: the plugin's is used");
                    return false;
                }
                String text = Files.readString(file, StandardCharsets.UTF_8);
                Files.writeString(path(tmp, schema, true), atSite(text), StandardCharsets.UTF_8);
            }
            Files.move(tmp, out, StandardCopyOption.ATOMIC_MOVE);
            tmp = null;
            return true;
        } catch (IOException e) {
            LOG.warn("cannot keep axx's schemas in " + out, e);
            return Files.isDirectory(out);
        } finally {
            if (tmp != null) {
                try {
                    NioFiles.deleteRecursively(tmp);
                } catch (IOException ignored) {
                    // a leftover in the IDE's cache
                }
            }
        }
    }

    /** Goes back to the plugin's schemas, and forgets which axx it asked; for tests. */
    void forget() {
        asked = null;
        use(null);
    }

    /** Puts the schemas axx gave (or, with null, the plugin's) in use, if they are not already. */
    private void use(@Nullable Given g) {
        if (Objects.equals(given, g)) {
            return;
        }
        if (g != null) {
            List<Path> files = new ArrayList<>();
            for (AxxSchemas.Schema schema : AxxSchemas.ALL) {
                files.add(path(g.dir(), schema, false));
                files.add(path(g.dir(), schema, true));
            }
            for (Path file : files) {
                LocalFileSystem.getInstance().refreshAndFindFileByNioFile(file);
            }
        }
        given = g;
        ApplicationManager.getApplication()
                .invokeLater(
                        () -> JsonSchemaService.Impl.get(project).reset(), project.getDisposed());
    }

    /** Removes the schemas of the axx this project ran on before. */
    private void removeOthers(Path keep) {
        try (Stream<Path> dirs = Files.list(keep.getParent())) {
            for (Path d : dirs.toList()) {
                if (!d.equals(keep) && Files.isDirectory(d) && !d.getFileName().toString().startsWith(".")) {
                    NioFiles.deleteRecursively(d);
                }
            }
        } catch (IOException ignored) {
            // a leftover in the IDE's cache
        }
    }

    /** Where this project's schemas are kept, in the IDE's cache. */
    private Path cacheDir() {
        String where = Objects.requireNonNullElse(project.getBasePath(), project.getName());
        return Path.of(PathManager.getSystemPath(), "axx", "schemas", sha256(where));
    }

    private static Path path(Path dir, AxxSchemas.Schema schema, boolean site) {
        return site ? dir.resolve("site").resolve(schema.file()) : dir.resolve(schema.file());
    }

    /** The same schema, whose $id is the docs site's address, which earlier axx.yaml files name. */
    static @NotNull String atSite(@NotNull String schema) {
        return schema.replace(GITHUB, SITE);
    }

    /** Whether a file holds a JSON object, as a schema is. */
    static boolean isSchema(@NotNull Path file) {
        try {
            return JsonParser.parseString(Files.readString(file, StandardCharsets.UTF_8)).isJsonObject();
        } catch (IOException | RuntimeException e) {
            return false;
        }
    }

    /** Runs a command in dir; its output, or null when it fails or takes too long. */
    private static @Nullable ProcessOutput run(List<String> command, Path dir) {
        try {
            GeneralCommandLine line =
                    new GeneralCommandLine(command)
                            .withWorkDirectory(dir.toFile())
                            .withParentEnvironmentType(GeneralCommandLine.ParentEnvironmentType.CONSOLE)
                            .withCharset(StandardCharsets.UTF_8);
            ProcessOutput out = ExecUtil.execAndGetOutput(line, TIMEOUT_MS);
            if (out.isTimeout() || out.getExitCode() != 0) {
                LOG.info("`" + String.join(" ", command) + "` failed: " + out.getStderr().strip());
                return null;
            }
            return out;
        } catch (ExecutionException e) {
            LOG.info("`" + String.join(" ", command) + "` could not run", e);
            return null;
        }
    }

    private static String sha256(String s) {
        try {
            byte[] sum = MessageDigest.getInstance("SHA-256").digest(s.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(sum, 0, 12);
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException(e);
        }
    }
}
