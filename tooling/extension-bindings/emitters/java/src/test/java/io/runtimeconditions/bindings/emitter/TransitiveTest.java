package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.TestSupport.compile;
import static io.runtimeconditions.bindings.emitter.TestSupport.delete;
import static io.runtimeconditions.bindings.emitter.TestSupport.developmentTarget;
import static io.runtimeconditions.bindings.emitter.TestSupport.modelPath;
import static io.runtimeconditions.bindings.emitter.TestSupport.temporaryDirectory;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.runtimeconditions.bindings.emitter.Plan.EmissionPlan;
import io.runtimeconditions.bindings.emitter.Target.Dependency;
import io.runtimeconditions.bindings.emitter.Target.PackageTarget;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import org.junit.jupiter.api.Test;

/** root -> middle -> leaf, where leaf owns the declaration the root implements. */
final class TransitiveTest {
    private static final String CASE = "03-transitive-closure";

    private record Package(PackageTarget target, ObjectNode model, EmissionPlan plan) {}

    private static PackageTarget target(String key, String root, List<Dependency> dependencies) {
        PackageTarget base = developmentTarget();
        return new PackageTarget(
                key,
                root,
                "io.runtimeconditions.transitive",
                key,
                "io.runtimeconditions.transitive." + key,
                base.version(),
                "bindings/" + key + "/java",
                base.minimumJavaVersion(),
                base.publicationMode(),
                base.repositoryUrl(),
                base.registryId(),
                dependencies,
                base.emitterSha256());
    }

    private static Dependency dependency(String extension, PackageTarget target) {
        return new Dependency(extension, target.groupId(), target.artifactId(), target.javaPackage(), target.version());
    }

    private static JsonNode extension(ObjectNode model, String id) {
        for (JsonNode item : Json.list(model.path("extensions"))) {
            if (item.path("id").asText().equals(id)) {
                return item;
            }
        }
        throw new IllegalStateException(id);
    }

    private static String string(JsonNode node) {
        return node.asText();
    }

    @Test
    void threeGeneratedPackagesCompileTogether() throws Exception {
        ObjectNode model = Model.load(modelPath(CASE));
        String rootId = model.path("rootExtension").path("id").asText();
        String leafId = string(model.path("vocabulary").path("importedDeclarations").get(0).path("owner"));
        String middleId = null;
        for (JsonNode item : Json.list(model.path("extensions"))) {
            for (JsonNode dependency : Json.list(item.path("dependencies"))) {
                if (dependency.asText().equals(leafId)) {
                    middleId = item.path("id").asText();
                }
            }
        }
        assertTrue(middleId != null);

        ObjectNode leafModel = model.deepCopy();
        leafModel.set("rootExtension", extension(model, leafId).deepCopy());
        ((ArrayNode) leafModel.path("extensions")).removeAll();
        ((ArrayNode) leafModel.path("extensions")).add(extension(model, leafId).deepCopy());
        ((ObjectNode) leafModel.path("vocabulary")).set("ownedDeclarations", model.path("vocabulary").path("importedDeclarations").deepCopy());
        ((ObjectNode) leafModel.path("vocabulary")).set("importedDeclarations", Json.MAPPER.createArrayNode());
        PackageTarget leafTarget = target("leaf", leafId, List.of());

        ObjectNode middleModel = model.deepCopy();
        middleModel.set("rootExtension", extension(model, middleId).deepCopy());
        ((ArrayNode) middleModel.path("extensions")).removeAll();
        ((ArrayNode) middleModel.path("extensions")).add(extension(model, leafId).deepCopy());
        ((ArrayNode) middleModel.path("extensions")).add(extension(model, middleId).deepCopy());
        ((ObjectNode) middleModel.path("vocabulary")).set("ownedDeclarations", Json.MAPPER.createArrayNode());
        PackageTarget middleTarget = target("middle", middleId, List.of(dependency(leafId, leafTarget)));

        // The root's target names the middle package it depends on and the leaf package that owns the marker.
        PackageTarget rootTarget =
                target("root", rootId, List.of(dependency(leafId, leafTarget), dependency(middleId, middleTarget)));

        Path workspace = temporaryDirectory();
        try {
            List<Path> sources = new java.util.ArrayList<>();
            Package[] packages = {
                new Package(leafTarget, leafModel, Plan.build(leafModel, leafTarget)),
                new Package(middleTarget, middleModel, Plan.build(middleModel, middleTarget)),
                new Package(rootTarget, model, Plan.build(model, rootTarget))
            };
            for (Package item : packages) {
                Path directory = workspace.resolve(item.target().packageKey());
                Source.emitPackage(item.plan(), item.model(), directory);
                Verification.verifyPackage(item.plan(), directory, item.model());
                sources.add(directory.resolve("src/main/java"));
            }
            StringBuilder root = new StringBuilder();
            try (java.util.stream.Stream<Path> walk = Files.walk(workspace.resolve("root/src/main/java"))) {
                for (Path file : walk.filter(item -> item.toString().endsWith(".java")).sorted().toList()) {
                    root.append(Files.readString(file));
                }
            }
            assertTrue(root.toString().contains("import io.runtimeconditions.transitive.leaf.WorkerField;"), root.toString());
            String pom = Files.readString(workspace.resolve("root/pom.xml"));
            assertTrue(pom.contains("<artifactId>leaf</artifactId>") && pom.contains("<artifactId>middle</artifactId>"), pom);
            // The middle package declares nothing of its own, so it has no Java source directory.
            assertTrue(!Files.exists(workspace.resolve("middle/src/main/java")));
            TestSupport.Compiled compiled = compile(sources, workspace.resolve("classes"));
            assertTrue(compiled.success(), compiled.output());
        } finally {
            delete(workspace);
        }
    }

    @Test
    void anImportedDeclarationOwnerWithoutAnEntryIsRejected() {
        ObjectNode model = Model.load(modelPath(CASE));
        String rootId = model.path("rootExtension").path("id").asText();
        String middleId = string(extension(model, rootId).path("dependencies").get(0));
        PackageTarget middleTarget = target("middle", middleId, List.of());
        PackageTarget rootTarget = target("root", rootId, List.of(dependency(middleId, middleTarget)));
        DiagnosticError missing = assertThrows(DiagnosticError.class, () -> Plan.build(model, rootTarget));
        assertEquals("RCJ1011", missing.diagnostic().code());

        PackageTarget stranger = target("stranger", "https://example.com/unrelated:1.0.0", List.of());
        PackageTarget withExtra =
                target("root", rootId, List.of(dependency(middleId, middleTarget), dependency(stranger.rootExtension(), stranger)));
        DiagnosticError extra = assertThrows(DiagnosticError.class, () -> Plan.build(model, withExtra));
        assertEquals("RCJ1009", extra.diagnostic().code());
    }
}
