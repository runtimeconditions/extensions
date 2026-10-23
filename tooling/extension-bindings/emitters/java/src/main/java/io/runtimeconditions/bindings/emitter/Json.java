package io.runtimeconditions.bindings.emitter;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.ArrayList;
import java.util.List;

final class Json {
    static final ObjectMapper MAPPER = new ObjectMapper();

    private Json() {}

    static List<JsonNode> list(JsonNode node) {
        List<JsonNode> result = new ArrayList<>();
        if (node != null && node.isArray()) {
            for (JsonNode item : (ArrayNode) node) {
                result.add(item);
            }
        }
        return result;
    }

    static String text(JsonNode node, String name) {
        JsonNode value = node.path(name);
        return value.isTextual() ? value.asText() : "";
    }

    static boolean has(JsonNode node, String name) {
        return node.has(name) && !node.get(name).isNull();
    }

    static ObjectNode object() {
        return MAPPER.createObjectNode();
    }

    static int compare(String left, String right) {
        return Integer.signum(left.compareTo(right));
    }
}
