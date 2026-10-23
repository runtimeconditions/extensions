package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.TestSupport.EMITTER_ROOT;
import static io.runtimeconditions.bindings.emitter.TestSupport.TARGET;
import static io.runtimeconditions.bindings.emitter.TestSupport.TOOLING;
import static io.runtimeconditions.bindings.emitter.TestSupport.buildCase;
import static io.runtimeconditions.bindings.emitter.TestSupport.delete;
import static io.runtimeconditions.bindings.emitter.TestSupport.temporaryDirectory;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.networknt.schema.JsonSchema;
import io.runtimeconditions.bindings.emitter.TestSupport.Built;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import java.util.function.Consumer;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import org.junit.jupiter.api.Test;
import org.snakeyaml.engine.v2.api.Load;
import org.snakeyaml.engine.v2.api.LoadSettings;

class ContractTest {
    private static String read(Path path) {
        try {
            return Files.readString(path, StandardCharsets.UTF_8);
        } catch (IOException error) {
            throw new IllegalStateException(error);
        }
    }

    private static JsonNode yaml(String text) {
        return Json.MAPPER.valueToTree(new Load(LoadSettings.builder().build()).loadFromString(text));
    }

    private static JsonSchema schema(Path path) {
        return Model.createValidator(read(path));
    }

    private static boolean valid(JsonSchema schema, JsonNode value) {
        return schema.validate(value).isEmpty();
    }

    private static final JsonSchema MANIFEST = schema(TOOLING.resolve("model/runtimeconditions.binding-manifest.schema.yaml"));

    @Test
    void thePackageTargetSchemaAcceptsTheTestTargetAndRejectsDrift() {
        JsonSchema validate = schema(EMITTER_ROOT.resolve("package-target.schema.yaml"));
        ObjectNode target = (ObjectNode) yaml(read(TARGET));
        assertTrue(validate.validate(target).isEmpty(), validate.validate(target).toString());
        List<Map<String, Object>> changes =
                List.of(
                        Map.of("sourceDirectory", "bindings/other/python"),
                        Map.of("minimumJavaVersion", "11"),
                        Map.of("groupId", "Bad Group"),
                        Map.of("javaPackage", "Bad.Package"),
                        Map.of("publicationMode", "registry"),
                        Map.of("extra", true));
        for (Map<String, Object> change : changes) {
            ObjectNode copy = target.deepCopy();
            change.forEach((key, value) -> copy.set(key, Json.MAPPER.valueToTree(value)));
            assertFalse(valid(validate, copy), change.toString());
        }
    }

    private static ObjectNode manifestOf(Built built) {
        return (ObjectNode) yaml(read(built.directory().resolve(Metadata.MANIFEST_PATH)));
    }

    @Test
    void everyEmittedManifestValidatesAgainstTheSharedBindingManifestSchema() {
        Path workspace = temporaryDirectory();
        try {
            for (String name :
                    List.of(
                            "01-owned-kind-interface",
                            "02-additive-field",
                            "06-recursive-reference",
                            "08-heterogeneous-union",
                            "09-collections-and-maps",
                            "10-scoped-domains-collisions")) {
                Built built = buildCase(name, workspace, null);
                var errors = MANIFEST.validate(manifestOf(built));
                assertTrue(errors.isEmpty(), name + ": " + errors);
            }
        } finally {
            delete(workspace);
        }
    }

    @Test
    void theManifestSchemaKeepsEachLanguagesIdentitySeparate() {
        Path workspace = temporaryDirectory();
        try {
            ObjectNode manifest = manifestOf(buildCase("01-owned-kind-interface", workspace, null));
            Map<String, Consumer<ObjectNode>> mutations =
                    Map.of(
                            "a Java manifest needs minimumJavaVersion", value -> ((ObjectNode) value.path("package")).remove("minimumJavaVersion"),
                            "a Java manifest cannot carry a Python version", value -> ((ObjectNode) value.path("package")).put("minimumPythonVersion", "3.12"),
                            "a Java manifest cannot name a Python file", value -> ((ObjectNode) value.path("declarations").get(0)).put("file", "bindings.py"),
                            "a Java package name must be a Java package", value -> ((ObjectNode) value.path("package")).put("name", "Bad Name"),
                            "a Java declaration must name its function class", value -> ((ObjectNode) value.path("declarations").get(0)).remove("functionClass"),
                            "an unknown language is rejected", value -> ((ObjectNode) value.path("package")).put("language", "ruby"));
            for (Map.Entry<String, Consumer<ObjectNode>> mutation : mutations.entrySet()) {
                ObjectNode copy = manifest.deepCopy();
                mutation.getValue().accept(copy);
                assertFalse(valid(MANIFEST, copy), mutation.getKey());
            }
            ObjectNode python = manifest.deepCopy();
            ObjectNode pack = Json.object();
            pack.put("language", "python");
            pack.put("coordinate", "x");
            pack.put("name", "x");
            pack.put("version", "1.0.0");
            pack.put("minimumJavaVersion", "17");
            python.set("package", pack);
            assertFalse(valid(MANIFEST, python), "a Python manifest cannot carry a Java version");
            ObjectNode go = manifest.deepCopy();
            ((ObjectNode) go.path("package")).put("language", "go");
            assertFalse(valid(MANIFEST, go));
            assertTrue(manifest.path("importedMarkerContracts") instanceof ArrayNode);
        } finally {
            delete(workspace);
        }
    }

    @Test
    void theEmittersToolPinsEqualTheSharedToolchainLock() {
        JsonNode lock = yaml(read(TOOLING.resolve("toolchain.lock.yaml")));
        JsonNode java = lock.path("languages").path("java");
        JsonNode tools = java.path("tools");
        assertEquals("maven", java.path("packageManager").asText());
        assertEquals("gradle", java.path("buildBackend").asText());
        Matcher plugin = Pattern.compile("<artifactId>maven-compiler-plugin</artifactId>\\s*<version>([^<]+)</version>").matcher(Templates.read("pom.xml.tmpl"));
        assertTrue(plugin.find());
        assertEquals(plugin.group(1), tools.path("maven-compiler-plugin").asText());
        String build = read(EMITTER_ROOT.resolve("build.gradle.kts"));
        Matcher matcher = Pattern.compile("\"([A-Za-z0-9_.-]+):([A-Za-z0-9_.-]+):([0-9][A-Za-z0-9_.-]*)\"").matcher(build);
        int matched = 0;
        while (matcher.find()) {
            String artifact = matcher.group(2);
            assertEquals(tools.path(artifact).asText(), matcher.group(3), artifact + " matches the lock");
            matched += 1;
        }
        assertTrue(matched >= 4, "build.gradle.kts pins at least the runtime dependencies");
        assertEquals(tools.path("gradle").asText(), gradleVersion());
    }

    @Test
    void identityComesFromTheBuildAndDependencyRangesCanBeOverridden() {
        assertEquals("runtimeconditions-binding-emitter-java", Metadata.EMITTER_NAME);
        assertTrue(Pattern.matches("\\d+\\.\\d+\\.\\d+", Metadata.EMITTER_VERSION), Metadata.EMITTER_VERSION);
        Target.Dependency plain = new Target.Dependency("e", "g", "a", "p", "1.2.3");
        Target.Dependency pinned = new Target.Dependency("e", "g", "a", "p", "1.2.3", "[1.2,2)");
        assertEquals("[1.2.3,2.0.0)", Metadata.dependencyVersion(plain));
        assertEquals("[1.2,2)", Metadata.dependencyVersion(pinned));
    }

    private static String gradleVersion() {
        return System.getProperty("rc.gradle.version", "");
    }
}
