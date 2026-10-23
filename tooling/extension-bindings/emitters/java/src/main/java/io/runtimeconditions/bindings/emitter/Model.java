package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.DiagnosticError.fail;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.networknt.schema.JsonSchema;
import com.networknt.schema.JsonSchemaFactory;
import com.networknt.schema.PathType;
import com.networknt.schema.SchemaValidatorsConfig;
import com.networknt.schema.SpecVersion;
import com.networknt.schema.ValidationMessage;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Set;
import org.snakeyaml.engine.v2.api.Load;
import org.snakeyaml.engine.v2.api.LoadSettings;

public final class Model {
    public static final String API = "runtimeconditions.io/binding-model/v1alpha1";
    public static final String KIND = "RuntimeConditionsBindingModel";
    private static final String SCHEMA_RESOURCE = "/model/runtimeconditions.binding-model.schema.yaml";

    private static JsonSchema validator;

    private Model() {}

    static String schemaText() {
        String override = System.getenv("RC_BINDING_MODEL_SCHEMA");
        try {
            if (override != null && !override.isEmpty()) {
                return Files.readString(Path.of(override), StandardCharsets.UTF_8);
            }
            try (InputStream stream = Model.class.getResourceAsStream(SCHEMA_RESOURCE)) {
                if (stream == null) {
                    throw fail("model", "RCJ1003", "model", "model schema is unavailable");
                }
                return new String(stream.readAllBytes(), StandardCharsets.UTF_8);
            }
        } catch (IOException error) {
            throw fail("model", "RCJ1003", "model", "model schema is unavailable");
        }
    }

    public static JsonSchema createValidator(String schemaYaml) {
        Object loaded = new Load(LoadSettings.builder().build()).loadFromString(schemaYaml);
        JsonNode schema = Json.MAPPER.valueToTree(loaded);
        SchemaValidatorsConfig config =
                SchemaValidatorsConfig.builder()
                        .pathType(PathType.JSON_POINTER)
                        .formatAssertionsEnabled(true)
                        .build();
        return JsonSchemaFactory.getInstance(SpecVersion.VersionFlag.V202012)
                .getSchema(schema, config);
    }

    public static synchronized JsonSchema validator() {
        if (validator == null) {
            validator = createValidator(schemaText());
        }
        return validator;
    }

    public static List<ValidationMessage> validate(JsonSchema schema, JsonNode model) {
        Set<ValidationMessage> messages = schema.validate(model);
        List<ValidationMessage> ordered = new ArrayList<>(messages);
        ordered.sort(
                Comparator.comparing((ValidationMessage item) -> item.getInstanceLocation().toString())
                        .thenComparing(ValidationMessage::getMessage));
        return ordered;
    }

    public static ObjectNode load(Path path) {
        ObjectNode model = YamlIo.load(path, true);
        if (!API.equals(model.path("apiVersion").asText(null))) {
            throw fail("model", "RCJ1004", "model", "unsupported binding-model apiVersion", "/apiVersion");
        }
        if (!KIND.equals(model.path("kind").asText(null))) {
            throw fail("model", "RCJ1005", "model", "unsupported binding-model kind", "/kind");
        }
        List<ValidationMessage> errors = validate(validator(), model);
        if (!errors.isEmpty()) {
            ValidationMessage error = errors.get(0);
            throw fail(
                    "model",
                    "RCJ1003",
                    "model",
                    error.getMessage(),
                    error.getInstanceLocation().toString());
        }
        return model;
    }
}
