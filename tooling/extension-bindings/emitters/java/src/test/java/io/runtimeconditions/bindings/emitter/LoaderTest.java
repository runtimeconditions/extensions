package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.TestSupport.TARGET;
import static io.runtimeconditions.bindings.emitter.TestSupport.delete;
import static io.runtimeconditions.bindings.emitter.TestSupport.modelPath;
import static io.runtimeconditions.bindings.emitter.TestSupport.temporaryDirectory;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.function.Consumer;
import org.junit.jupiter.api.Test;

class LoaderTest {
    private static final String GOOD = read(modelPath("01-owned-kind-interface"));

    private static String read(Path path) {
        try {
            return Files.readString(path, StandardCharsets.UTF_8);
        } catch (IOException error) {
            throw new IllegalStateException(error);
        }
    }

    private static void expect(Runnable action, String code, String message) {
        DiagnosticError error = assertThrows(DiagnosticError.class, action::run);
        assertEquals(code, error.diagnostic().code(), error.getMessage());
        if (message != null) {
            assertTrue(error.diagnostic().message().contains(message) || error.diagnostic().message().matches("(?s).*" + message + ".*"), error.getMessage());
        }
    }

    private static void withFile(String content, Consumer<Path> action) {
        Path directory = temporaryDirectory();
        try {
            Path path = directory.resolve("input.yaml");
            Files.writeString(path, content, StandardCharsets.UTF_8);
            action.accept(path);
        } catch (IOException error) {
            throw new IllegalStateException(error);
        } finally {
            delete(directory);
        }
    }

    @Test
    void theCommittedConformanceModelLoads() {
        withFile(GOOD, path -> assertEquals("RuntimeConditionsBindingModel", Model.load(path).path("kind").asText()));
    }

    @Test
    void aBindingModelRejectsABomCrlfAndMissingOrDoubledTrailingNewlines() {
        List<String> cases =
                List.of("﻿" + GOOD, GOOD.replace("\n", "\r\n"), GOOD.substring(0, GOOD.length() - 1), GOOD + "\n", "");
        for (String content : cases) {
            withFile(content, path -> expect(() -> Model.load(path), "RCJ1002", "without BOM and LF"));
        }
    }

    @Test
    void aBindingModelRejectsCommentsAnchorsAliasesTagsAndDuplicateKeys() {
        withFile(
                GOOD.replace("kind: RuntimeConditionsBindingModel", "kind: RuntimeConditionsBindingModel # note"),
                path -> expect(() -> Model.load(path), "RCJ1002", "comments"));
        withFile("# leading\n" + GOOD, path -> expect(() -> Model.load(path), "RCJ1002", "comments"));
        withFile(
                GOOD.replace("kind: RuntimeConditionsBindingModel", "kind: &a RuntimeConditionsBindingModel"),
                path -> expect(() -> Model.load(path), "RCJ1002", "anchors"));
        withFile(
                GOOD.replace("kind: RuntimeConditionsBindingModel", "kind: !!str RuntimeConditionsBindingModel"),
                path -> expect(() -> Model.load(path), "RCJ1002", "tags"));
        withFile(GOOD + "kind: RuntimeConditionsBindingModel\n", path -> expect(() -> Model.load(path), "RCJ1002", null));
        withFile("x: &a 1\ny: *a\n", path -> expect(() -> Model.load(path), "RCJ1002", "aliases"));
    }

    @Test
    void aBindingModelRejectsEmptyMultiDocumentNonMappingAndNonStringKeyInput() {
        withFile("\n", path -> expect(() -> Model.load(path), "RCJ1002", "empty"));
        withFile("a: 1\n---\nb: 2\n", path -> expect(() -> Model.load(path), "RCJ1002", "exactly one document"));
        withFile("- 1\n- 2\n", path -> expect(() -> Model.load(path), "RCJ1002", "mapping"));
        withFile("1: x\n", path -> expect(() -> Model.load(path), "RCJ1002", "keys must be strings"));
        withFile("a: .nan\n", path -> expect(() -> Model.load(path), "RCJ1002", "non-finite"));
    }

    @Test
    void aMissingFileIsReportedAsUnreadableInput() {
        expect(() -> Model.load(Path.of("/nonexistent/model.yaml")), "RCJ1001", "cannot read");
    }

    private static String mutated(java.util.function.Consumer<ObjectNode> change) {
        ObjectNode value = Model.load(modelPath("01-owned-kind-interface")).deepCopy();
        change.accept(value);
        return TestSupport.yaml(value);
    }

    @Test
    void unsupportedModelVersionsAndShapesAreRejectedWithDistinctCodes() {
        withFile(
                mutated(value -> value.put("apiVersion", "runtimeconditions.io/binding-model/v9")),
                path -> expect(() -> Model.load(path), "RCJ1004", null));
        withFile(mutated(value -> value.put("kind", "Other")), path -> expect(() -> Model.load(path), "RCJ1005", null));
        withFile(
                mutated(value -> ((ObjectNode) value.path("schemas").get(0).path("projection")).put("kind", "tuple")),
                path -> expect(() -> Model.load(path), "RCJ1003", null));
        withFile(mutated(value -> value.remove("vocabulary")), path -> expect(() -> Model.load(path), "RCJ1003", null));
        withFile(mutated(value -> value.put("surprise", true)), path -> expect(() -> Model.load(path), "RCJ1003", null));
    }

    private static void badTarget(String base, String from, String to, String code, String message) {
        assertTrue(base.contains(from), from);
        withFile(base.replace(from, to), path -> expect(() -> Target.load(path), code, message));
    }

    @Test
    void packageTargetsAreValidatedFieldByField() {
        String base = read(TARGET);
        assertEquals("conformance-owned-kind-interface", Target.load(TARGET).artifactId());
        String[][] bad = {
            {"packageKey: conformance-owned-kind-interface", "packageKey: Bad_Key", "RCJ1006", null},
            {"groupId: io.runtimeconditions.bindings", "groupId: Bad_Group", "RCJ1006", null},
            {"artifactId: conformance-owned-kind-interface", "artifactId: Bad Artifact", "RCJ1006", null},
            {"javaPackage: io.runtimeconditions.bindings.conformance.ownedkindinterface", "javaPackage: io.class.bindings", "RCJ1006", null},
            {"javaPackage: io.runtimeconditions.bindings.conformance.ownedkindinterface", "javaPackage: Io.Bad", "RCJ1006", null},
            {"version: 1.0.0", "version: 1.0", "RCJ1006", null},
            {"sourceDirectory: bindings/conformance-owned-kind-interface/java", "sourceDirectory: bindings/other/java", "RCJ1007", null},
            {"minimumJavaVersion: '17'", "minimumJavaVersion: '11'", "RCJ1008", null},
            {"minimumJavaVersion: '17'", "minimumJavaVersion: '17.0'", "RCJ1008", null},
            {"publicationMode: github-tag", "publicationMode: registry", "RCJ1006", "registryId"},
            {"publicationMode: github-tag", "publicationMode: other", "RCJ1006", null},
            {"repositoryUrl: https://github.com/runtimeconditions/extensions", "repositoryUrl: http://example.com/x", "RCJ1006", null},
            {"kind: RuntimeConditionsJavaPackageTarget", "kind: Other", "RCJ1006", null},
        };
        for (String[] item : bad) {
            badTarget(base, item[0], item[1], item[2], item[3]);
        }
        withFile(base + "extra: 1\n", path -> expect(() -> Target.load(path), "RCJ1006", "unknown field 'extra'"));
        withFile(base.replaceFirst("(?m)^repositoryUrl:.*\n", ""), path -> expect(() -> Target.load(path), "RCJ1006", "missing required field"));
        withFile(base + "emitterSha256: nothex\n", path -> expect(() -> Target.load(path), "RCJ1006", "emitterSha256"));
        withFile(base + "registryId: central\n", path -> expect(() -> Target.load(path), "RCJ1006", "registryId"));
        String entry = "dependencies:\n";
        withFile(
                base + entry + "  - extension: a\n    groupId: g\n    artifactId: dep\n    javaPackage: dep.one\n    version: 1.0.0\n"
                        + "  - extension: a\n    groupId: g\n    artifactId: dep2\n    javaPackage: dep.two\n    version: 1.0.0\n",
                path -> expect(() -> Target.load(path), "RCJ1006", "duplicate dependency"));
        withFile(base + entry + "  - extension: a\n    groupId: g\n", path -> expect(() -> Target.load(path), "RCJ1006", "missing dependency field"));
        withFile(
                base + entry + "  - extension: a\n    groupId: g\n    artifactId: Bad\n    javaPackage: dep.one\n    version: 1.0.0\n",
                path -> expect(() -> Target.load(path), "RCJ1006", "invalid dependency"));
        withFile(base + entry + "  - bad\n", path -> expect(() -> Target.load(path), "RCJ1006", "mapping"));
    }
}
