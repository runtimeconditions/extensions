package io.runtimeconditions.bindings.emitter;

import com.fasterxml.jackson.databind.node.ObjectNode;
import io.runtimeconditions.bindings.emitter.Plan.EmissionPlan;
import io.runtimeconditions.bindings.emitter.Plan.ImportedDeclarationPlan;
import io.runtimeconditions.bindings.emitter.Target.Dependency;
import io.runtimeconditions.bindings.emitter.Target.PackageTarget;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import java.util.stream.Stream;
import javax.tools.Diagnostic;
import javax.tools.DiagnosticCollector;
import javax.tools.JavaCompiler;
import javax.tools.JavaFileObject;
import javax.tools.StandardJavaFileManager;
import javax.tools.ToolProvider;

final class TestSupport {
    static final Path EMITTER_ROOT = Path.of(System.getProperty("rc.emitter.root")).toAbsolutePath().normalize();
    static final Path TOOLING = EMITTER_ROOT.resolve("../..").normalize();
    static final Path MODELS = TOOLING.resolve("model/conformance/expected");
    static final Path TARGET = EMITTER_ROOT.resolve("testdata/package-target.yaml");

    private TestSupport() {}

    static List<String> cases() {
        try (Stream<Path> entries = Files.list(MODELS)) {
            return entries
                    .filter(path -> Files.exists(path.resolve("runtimeconditions.binding-model.yaml")))
                    .map(path -> path.getFileName().toString())
                    .sorted()
                    .toList();
        } catch (IOException error) {
            throw new UncheckedIOException(error);
        }
    }

    static Path modelPath(String name) {
        return MODELS.resolve(name).resolve("runtimeconditions.binding-model.yaml");
    }

    static Path temporaryDirectory() {
        try {
            return Files.createTempDirectory("rc-java-emitter-");
        } catch (IOException error) {
            throw new UncheckedIOException(error);
        }
    }

    static void delete(Path directory) {
        if (!Files.exists(directory)) {
            return;
        }
        try (Stream<Path> walk = Files.walk(directory)) {
            walk.sorted(Comparator.reverseOrder()).forEach(path -> path.toFile().delete());
        } catch (IOException error) {
            throw new UncheckedIOException(error);
        }
    }

    static String sha256(byte[] data) {
        try {
            StringBuilder hex = new StringBuilder();
            for (byte value : MessageDigest.getInstance("SHA-256").digest(data)) {
                hex.append(String.format("%02x", value));
            }
            return hex.toString();
        } catch (java.security.NoSuchAlgorithmException error) {
            throw new IllegalStateException(error);
        }
    }

    static String emitterDigest() {
        Map<String, String> inventory = new TreeMap<>();
        try (Stream<Path> walk = Files.walk(EMITTER_ROOT.resolve("src/main/java"))) {
            for (Path path : walk.filter(item -> item.toString().endsWith(".java")).sorted().toList()) {
                inventory.put(EMITTER_ROOT.relativize(path).toString(), sha256(Files.readAllBytes(path)));
            }
            try (Stream<Path> templates = Files.walk(EMITTER_ROOT.resolve("templates"))) {
                for (Path path : templates.filter(Files::isRegularFile).sorted().toList()) {
                    inventory.put(EMITTER_ROOT.relativize(path).toString(), sha256(Files.readAllBytes(path)));
                }
            }
            inventory.put("package-target.schema.yaml", sha256(Files.readAllBytes(EMITTER_ROOT.resolve("package-target.schema.yaml"))));
            inventory.put("build.gradle.kts", sha256(Files.readAllBytes(EMITTER_ROOT.resolve("build.gradle.kts"))));
            return sha256(Json.MAPPER.writeValueAsString(inventory).getBytes(StandardCharsets.UTF_8));
        } catch (IOException error) {
            throw new UncheckedIOException(error);
        }
    }

    static PackageTarget developmentTarget() {
        return Target.load(TARGET).withEmitterSha256(emitterDigest());
    }

    static String rootOf(ObjectNode model) {
        return model.path("rootExtension").path("id").asText();
    }

    static List<String> directDependencies(ObjectNode model) {
        List<String> result = new ArrayList<>();
        for (var extension : Json.list(model.path("extensions"))) {
            if (extension.path("id").asText().equals(rootOf(model))) {
                for (var dependency : Json.list(extension.path("dependencies"))) {
                    result.add(dependency.asText());
                }
            }
        }
        return result;
    }

    static String javaPackageFor(String name) {
        return "io.runtimeconditions.conformance.c" + name.replaceAll("[^a-z0-9]", "");
    }

    static PackageTarget targetFor(String name, ObjectNode model, Map<String, Integer> providers) {
        PackageTarget base = developmentTarget();
        List<Dependency> dependencies = new ArrayList<>();
        for (Map.Entry<String, Integer> provider : providers.entrySet()) {
            int index = provider.getValue();
            dependencies.add(
                    new Dependency(
                            provider.getKey(), "io.runtimeconditions.providers", "provider-" + index, "provider.p" + index, "1.0.0"));
        }
        return new PackageTarget(
                name,
                rootOf(model),
                "io.runtimeconditions.conformance",
                name,
                javaPackageFor(name),
                base.version(),
                "bindings/" + name + "/java",
                base.minimumJavaVersion(),
                base.publicationMode(),
                base.repositoryUrl(),
                base.registryId(),
                dependencies,
                base.emitterSha256());
    }

    /** The direct dependencies and the owners of imported declarations, each with its own provider package. */
    static Map<String, Integer> providersOf(ObjectNode model) {
        Map<String, Integer> providers = new LinkedHashMap<>();
        for (String extension : directDependencies(model)) {
            providers.put(extension, providers.size());
        }
        for (var item : Json.list(model.path("vocabulary").path("importedDeclarations"))) {
            providers.putIfAbsent(item.path("owner").asText(), providers.size());
        }
        return providers;
    }

    static EmissionPlan planFor(ObjectNode model, String name) {
        return Plan.build(model, targetFor(name, model, providersOf(model)));
    }

    record Built(Path directory, Path stubs, ObjectNode model, EmissionPlan plan) {}

    static void writeProviderStubs(EmissionPlan plan, Path stubs, String markerOverride) {
        Map<String, List<ImportedDeclarationPlan>> byProvider = new TreeMap<>();
        for (ImportedDeclarationPlan item : plan.importedDeclarations()) {
            byProvider.computeIfAbsent(item.providerPackage().javaPackage(), key -> new ArrayList<>()).add(item);
        }
        try {
            for (Map.Entry<String, List<ImportedDeclarationPlan>> entry : byProvider.entrySet()) {
                Path directory = stubs.resolve(entry.getKey().replace('.', '/'));
                Files.createDirectories(directory);
                String header = "package " + entry.getKey() + ";\n\n";
                Files.writeString(directory.resolve("Declaration.java"), header + "public final class Declaration {\n    Declaration() {}\n}\n");
                StringBuilder functions = new StringBuilder(header + "public final class Bindings {\n    private Bindings() {}\n");
                for (ImportedDeclarationPlan item : entry.getValue()) {
                    Files.writeString(
                            directory.resolve(item.protocol() + ".java"),
                            header
                                    + "public interface "
                                    + item.protocol()
                                    + " {\n    void "
                                    + (markerOverride == null ? item.method() : markerOverride)
                                    + "();\n}\n");
                    functions
                            .append("\n    public static Declaration ")
                            .append(item.function())
                            .append("(")
                            .append(item.protocol())
                            .append("... fields) {\n        return new Declaration();\n    }\n");
                }
                functions.append("}\n");
                Files.writeString(directory.resolve("Bindings.java"), functions.toString());
            }
        } catch (IOException error) {
            throw new UncheckedIOException(error);
        }
    }

    static Built buildCase(String name, Path workspace, String markerOverride) {
        ObjectNode model = Model.load(modelPath(name));
        EmissionPlan plan = Plan.build(model, targetFor(name, model, providersOf(model)));
        Path stubs = workspace.resolve("stubs");
        writeProviderStubs(plan, stubs, markerOverride);
        Path directory = workspace.resolve("pkg").resolve(name);
        Source.emitPackage(plan, model, directory);
        return new Built(directory, stubs, model, plan);
    }

    static void writeConformance(Built built) {
        try {
            Files.writeString(
                    built.directory().resolve(Source.sourcePath(built.plan(), "Exercise")),
                    Conformance.source(built.plan(), built.model()),
                    StandardCharsets.UTF_8);
        } catch (IOException error) {
            throw new UncheckedIOException(error);
        }
    }

    record Compiled(boolean success, String output) {}

    static Compiled compile(List<Path> roots, Path classes) {
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        DiagnosticCollector<JavaFileObject> diagnostics = new DiagnosticCollector<>();
        List<Path> files = new ArrayList<>();
        for (Path root : roots) {
            if (!Files.exists(root)) {
                continue;
            }
            try (Stream<Path> walk = Files.walk(root)) {
                walk.filter(path -> path.toString().endsWith(".java")).sorted().forEach(files::add);
            } catch (IOException error) {
                throw new UncheckedIOException(error);
            }
        }
        try (StandardJavaFileManager manager = compiler.getStandardFileManager(diagnostics, null, StandardCharsets.UTF_8)) {
            Files.createDirectories(classes);
            List<String> options = List.of("--release", "17", "-Xlint:all", "-Werror", "-proc:none", "-d", classes.toString());
            boolean success =
                    compiler.getTask(null, manager, diagnostics, options, null, manager.getJavaFileObjectsFromPaths(files)).call();
            StringBuilder output = new StringBuilder();
            for (Diagnostic<? extends JavaFileObject> item : diagnostics.getDiagnostics()) {
                output.append(item.getKind()).append(' ').append(item.getMessage(null)).append('\n');
            }
            return new Compiled(success, output.toString());
        } catch (IOException error) {
            throw new UncheckedIOException(error);
        }
    }

    static Compiled compileBuilt(Built built, Path classes) {
        return compile(List.of(built.directory().resolve("src/main/java"), built.stubs()), classes);
    }

    static String yaml(com.fasterxml.jackson.databind.JsonNode node) {
        org.snakeyaml.engine.v2.api.DumpSettings settings =
                org.snakeyaml.engine.v2.api.DumpSettings.builder()
                        .setDefaultFlowStyle(org.snakeyaml.engine.v2.common.FlowStyle.BLOCK)
                        .setWidth(Integer.MAX_VALUE)
                        .setSplitLines(false)
                        .build();
        return new org.snakeyaml.engine.v2.api.Dump(settings).dumpToString(Json.MAPPER.convertValue(node, Object.class));
    }

    static Map<String, String> snapshot(Path directory) {
        Map<String, String> result = new TreeMap<>();
        try (Stream<Path> walk = Files.walk(directory)) {
            for (Path path : walk.filter(Files::isRegularFile).sorted().toList()) {
                result.put(directory.relativize(path).toString(), Files.readString(path, StandardCharsets.UTF_8));
            }
        } catch (IOException error) {
            throw new UncheckedIOException(error);
        }
        return result;
    }
}
