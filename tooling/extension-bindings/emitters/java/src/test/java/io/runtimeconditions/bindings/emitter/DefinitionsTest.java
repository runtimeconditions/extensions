package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.TestSupport.compileBuilt;
import static io.runtimeconditions.bindings.emitter.TestSupport.delete;
import static io.runtimeconditions.bindings.emitter.TestSupport.modelPath;
import static io.runtimeconditions.bindings.emitter.TestSupport.planFor;
import static io.runtimeconditions.bindings.emitter.TestSupport.temporaryDirectory;
import static io.runtimeconditions.bindings.emitter.TestSupport.writeConformance;
import static io.runtimeconditions.bindings.emitter.TestSupport.writeProviderStubs;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.runtimeconditions.bindings.emitter.Plan.EmissionPlan;
import io.runtimeconditions.bindings.emitter.TestSupport.Built;
import java.nio.file.Files;
import java.nio.file.Path;
import org.junit.jupiter.api.Test;

/** A property that references a definition which is not an object keeps that definition's type. */
final class DefinitionsTest {
    private static final String CASE = "06-recursive-reference";

    private static ObjectNode scalar(JsonNode provenance, String pointer, String kind, String scalar) {
        ObjectNode shape = Json.object();
        shape.put("kind", kind);
        if (scalar != null) {
            shape.put("scalar", scalar);
        }
        shape.set("provenance", provenance(provenance, pointer));
        return shape;
    }

    private static ObjectNode reference(JsonNode provenance, String pointer, String target) {
        ObjectNode shape = Json.object();
        shape.put("kind", "ref");
        shape.put("ref", "#/$defs/" + target);
        shape.set("provenance", provenance(provenance, pointer));
        return shape;
    }

    private static ObjectNode provenance(JsonNode base, String pointer) {
        ObjectNode copy = base.deepCopy();
        copy.put("jsonPointer", pointer);
        return copy;
    }

    private static void definition(ArrayNode definitions, JsonNode base, String name, ObjectNode shape) {
        ObjectNode item = definitions.addObject();
        item.put("name", name);
        item.put("jsonPointer", "/$defs/" + name);
        item.set("shape", shape);
        item.set("provenance", provenance(base, "/$defs/" + name));
    }

    private static void property(ArrayNode properties, JsonNode base, String name, ObjectNode shape) {
        ObjectNode item = properties.addObject();
        item.put("name", name);
        item.put("required", true);
        item.set("shape", shape);
        item.set("provenance", provenance(base, "/$defs/node/properties/" + name));
    }

    private static ObjectNode modelWithScalarDefinitions() {
        ObjectNode model = Model.load(modelPath(CASE));
        ObjectNode schema = (ObjectNode) model.path("schemas").get(0);
        ArrayNode definitions = (ArrayNode) schema.path("definitions");
        ObjectNode node = (ObjectNode) definitions.get(0);
        JsonNode base = node.path("provenance");
        definition(definitions, base, "label", scalar(base, "/$defs/label", "scalar", "string"));
        definition(definitions, base, "anything", scalar(base, "/$defs/anything", "any", null));
        definition(definitions, base, "labelAlias", reference(base, "/$defs/labelAlias", "label"));
        definition(definitions, base, "labelAliasAlias", reference(base, "/$defs/labelAliasAlias", "labelAlias"));
        ArrayNode properties = (ArrayNode) node.path("shape").path("properties");
        property(properties, base, "label", reference(base, "/$defs/node/properties/label", "label"));
        property(properties, base, "anything", reference(base, "/$defs/node/properties/anything", "anything"));
        property(properties, base, "chained", reference(base, "/$defs/node/properties/chained", "labelAliasAlias"));
        ObjectNode list = Json.object();
        list.put("kind", "array");
        list.set("items", reference(base, "/$defs/node/properties/labels/items", "labelAlias"));
        list.set("provenance", provenance(base, "/$defs/node/properties/labels"));
        property(properties, base, "labels", list);
        return model;
    }

    @Test
    void scalarAnyAndChainedDefinitionsKeepTheirResolvedTypes() throws Exception {
        Path workspace = temporaryDirectory();
        try {
            ObjectNode model = modelWithScalarDefinitions();
            EmissionPlan plan = planFor(model, CASE);
            Path stubs = workspace.resolve("stubs");
            writeProviderStubs(plan, stubs, null);
            Path directory = workspace.resolve("pkg").resolve(CASE);
            Source.emitPackage(plan, model, directory);
            Built built = new Built(directory, stubs, model, plan);
            Verification.verifyPackage(plan, directory, model);
            writeConformance(built);

            StringBuilder generated = new StringBuilder();
            try (java.util.stream.Stream<Path> walk = Files.walk(directory.resolve("src/main/java"))) {
                for (Path file : walk.filter(item -> item.toString().endsWith(".java")).sorted().toList()) {
                    generated.append(Files.readString(file));
                }
            }
            String source = generated.toString();
            assertFalse(source.contains("null "), "null annotation");
            assertTrue(source.contains("String label"), "String label");
            assertTrue(source.contains("Object anything"), "Object anything");
            assertTrue(source.contains("String chained"), "String chained");
            assertTrue(source.contains("List<String>"), "list of resolved strings");
            String manifest = Files.readString(directory.resolve(Metadata.MANIFEST_PATH));
            assertFalse(manifest.contains("type: null"), "null manifest type");
            assertTrue(compileBuilt(built, workspace.resolve("classes")).success());
        } finally {
            delete(workspace);
        }
    }

    @Test
    void aDefinitionThatOnlyRefersToItselfIsRejected() {
        ObjectNode model = modelWithScalarDefinitions();
        ObjectNode schema = (ObjectNode) model.path("schemas").get(0);
        ArrayNode definitions = (ArrayNode) schema.path("definitions");
        JsonNode base = definitions.get(0).path("provenance");
        definition(definitions, base, "loopA", reference(base, "/$defs/loopA", "loopB"));
        definition(definitions, base, "loopB", reference(base, "/$defs/loopB", "loopA"));
        ArrayNode properties = (ArrayNode) definitions.get(0).path("shape").path("properties");
        property(properties, base, "loop", reference(base, "/$defs/node/properties/loop", "loopA"));
        DiagnosticError error = org.junit.jupiter.api.Assertions.assertThrows(DiagnosticError.class, () -> planFor(model, CASE));
        assertEquals("RCJ1012", error.diagnostic().code());
    }
}
