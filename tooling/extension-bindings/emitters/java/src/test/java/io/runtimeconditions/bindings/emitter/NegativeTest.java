package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.TestSupport.EMITTER_ROOT;
import static io.runtimeconditions.bindings.emitter.TestSupport.developmentTarget;
import static io.runtimeconditions.bindings.emitter.TestSupport.modelPath;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.runtimeconditions.bindings.emitter.Target.PackageTarget;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.snakeyaml.engine.v2.api.Load;
import org.snakeyaml.engine.v2.api.LoadSettings;

class NegativeTest {
    private static JsonNode property(ObjectNode model, int index) {
        return model.path("schemas").get(0).path("projection").path("properties").get(index);
    }

    private static JsonNode named(JsonNode node, String name) {
        for (JsonNode item : Json.list(node.path("properties"))) {
            if (item.path("name").asText().equals(name)) {
                return item;
            }
        }
        throw new IllegalStateException(name);
    }

    private static void mutate(String name, ObjectNode model) {
        switch (name) {
            case "reserved-global" -> ((ObjectNode) property(model, 1)).put("name", "record");
            case "fixed-function-collision" -> {
                ObjectNode second = model.path("vocabulary").path("ownedDeclarations").get(0).deepCopy();
                second.put("coordinate", "kind:Service");
                second.put("kind", "Service");
                ((ArrayNode) model.path("vocabulary").path("ownedDeclarations")).add(second);
            }
            case "enum-member-collision" -> {
                ObjectNode shape = (ObjectNode) named(model.path("schemas").get(0).path("projection"), "mode").path("shape");
                String first = shape.path("values").get(0).path("value").asText();
                ((ArrayNode) shape.path("values")).addObject().put("value", first.toUpperCase());
                ObjectNode scoped = (ObjectNode) named(model.path("scopes").get(0).path("projection"), "mode").path("shape");
                scoped.removeAll();
                scoped.setAll(shape.deepCopy());
            }
            case "unnameable-field" -> ((ObjectNode) property(model, 1)).put("name", "💥");
            default -> throw new IllegalStateException(name);
        }
    }

    @Test
    @SuppressWarnings("unchecked")
    void negativeDiagnosticsMatchTheFixtures() throws IOException {
        String text = Files.readString(EMITTER_ROOT.resolve("testdata/negative-diagnostics.yaml"), StandardCharsets.UTF_8);
        List<Map<String, Object>> fixtures = (List<Map<String, Object>>) new Load(LoadSettings.builder().build()).loadFromString(text);
        assertTrue(fixtures.size() >= 4);
        for (Map<String, Object> fixture : fixtures) {
            ObjectNode model = Model.load(modelPath((String) fixture.get("positiveCase"))).deepCopy();
            mutate((String) fixture.get("case"), model);
            assertTrue(Model.validate(Model.validator(), model).isEmpty(), "the mutated model must stay schema-valid");
            PackageTarget target = developmentTarget();
            PackageTarget retargeted =
                    new PackageTarget(
                            target.packageKey(),
                            model.path("rootExtension").path("id").asText(),
                            target.groupId(),
                            target.artifactId(),
                            target.javaPackage(),
                            target.version(),
                            target.sourceDirectory(),
                            target.minimumJavaVersion(),
                            target.publicationMode(),
                            target.repositoryUrl(),
                            target.registryId(),
                            target.dependencies(),
                            target.emitterSha256());
            DiagnosticError error = assertThrows(DiagnosticError.class, () -> Plan.build(model, retargeted), (String) fixture.get("case"));
            Map<String, String> expected = (Map<String, String>) fixture.get("expected");
            DiagnosticError.Diagnostic actual = error.diagnostic();
            assertEquals(
                    expected,
                    Map.of(
                            "category", actual.category(),
                            "code", actual.code(),
                            "coordinate", actual.coordinate(),
                            "jsonPointer", actual.jsonPointer(),
                            "message", actual.message()),
                    (String) fixture.get("case"));
        }
    }
}
