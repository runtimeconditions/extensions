package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.DiagnosticError.fail;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import org.snakeyaml.engine.v2.api.Load;
import org.snakeyaml.engine.v2.api.LoadSettings;
import org.snakeyaml.engine.v2.comments.CommentType;
import org.snakeyaml.engine.v2.events.CommentEvent;
import org.snakeyaml.engine.v2.events.Event;
import org.snakeyaml.engine.v2.events.NodeEvent;
import org.snakeyaml.engine.v2.events.ScalarEvent;
import org.snakeyaml.engine.v2.events.CollectionStartEvent;
import org.snakeyaml.engine.v2.exceptions.YamlEngineException;
import org.snakeyaml.engine.v2.parser.ParserImpl;
import org.snakeyaml.engine.v2.scanner.StreamReader;

final class YamlIo {
    static final int MAX_BYTES = 64 * 1024 * 1024;
    static final int MAX_NODES = 1_000_000;
    static final int MAX_DEPTH = 256;
    static final int MAX_ALIASES = 100;

    private static final String BAD_MODEL_BYTES =
            "binding model must use UTF-8 without BOM and LF with one trailing newline";
    private static final String BAD_MODEL_NODES =
            "binding model may not contain YAML anchors, aliases, or tags";

    private YamlIo() {}

    private static String strictDecode(byte[] data, String coordinate) {
        try {
            return StandardCharsets.UTF_8
                    .newDecoder()
                    .onMalformedInput(CodingErrorAction.REPORT)
                    .onUnmappableCharacter(CodingErrorAction.REPORT)
                    .decode(ByteBuffer.wrap(data))
                    .toString();
        } catch (CharacterCodingException error) {
            throw fail("input", "RCJ1002", coordinate, "input is not valid UTF-8");
        }
    }

    private static void checkModelBytes(byte[] data, String coordinate) {
        boolean bom =
                data.length >= 3
                        && (data[0] & 0xff) == 0xef
                        && (data[1] & 0xff) == 0xbb
                        && (data[2] & 0xff) == 0xbf;
        boolean carriageReturn = false;
        for (byte value : data) {
            if (value == 0x0d) {
                carriageReturn = true;
                break;
            }
        }
        if (bom
                || carriageReturn
                || data.length == 0
                || data[data.length - 1] != 0x0a
                || (data.length > 1 && data[data.length - 2] == 0x0a)) {
            throw fail("input", "RCJ1002", coordinate, BAD_MODEL_BYTES);
        }
    }

    private static String firstLine(String message) {
        int end = message.indexOf('\n');
        return end < 0 ? message : message.substring(0, end);
    }

    private static void scanEvents(String source, boolean model, String coordinate) {
        LoadSettings settings =
                LoadSettings.builder()
                        .setParseComments(model)
                        .setAllowDuplicateKeys(false)
                        .setMaxAliasesForCollections(MAX_ALIASES)
                        .build();
        int documents = 0;
        int nodes = 0;
        int aliases = 0;
        int depth = 0;
        try {
            ParserImpl parser = new ParserImpl(settings, new StreamReader(settings, source));
            while (parser.hasNext()) {
                Event event = parser.next();
                switch (event.getEventId()) {
                    case Comment -> {
                        if (model && ((CommentEvent) event).getCommentType() != CommentType.BLANK_LINE) {
                            throw fail(
                                    "input",
                                    "RCJ1002",
                                    coordinate,
                                    "binding model may not contain YAML comments");
                        }
                    }
                    case DocumentStart -> documents += 1;
                    case Alias -> {
                        nodes += 1;
                        aliases += 1;
                        if (model) {
                            throw fail(
                                    "input",
                                    "RCJ1002",
                                    coordinate,
                                    "binding model may not contain YAML aliases");
                        }
                        if (aliases > MAX_ALIASES) {
                            throw fail("input", "RCJ1002", coordinate, "YAML alias limit exceeded");
                        }
                    }
                    case Scalar, SequenceStart, MappingStart -> {
                        nodes += 1;
                        if (event.getEventId() != Event.ID.Scalar) {
                            depth += 1;
                        }
                        if (nodes > MAX_NODES) {
                            throw fail("input", "RCJ1002", coordinate, "YAML node limit exceeded");
                        }
                        if (depth > MAX_DEPTH) {
                            throw fail(
                                    "input", "RCJ1002", coordinate, "YAML nesting limit exceeded");
                        }
                        if (model && event instanceof NodeEvent node && node.getAnchor().isPresent()) {
                            throw fail("input", "RCJ1002", coordinate, BAD_MODEL_NODES);
                        }
                        if (model && hasExplicitTag(event)) {
                            throw fail("input", "RCJ1002", coordinate, BAD_MODEL_NODES);
                        }
                    }
                    case SequenceEnd, MappingEnd -> depth -= 1;
                    default -> {}
                }
            }
        } catch (YamlEngineException error) {
            throw fail("input", "RCJ1002", coordinate, firstLine(String.valueOf(error.getMessage())));
        }
        if (documents == 0) {
            throw fail("input", "RCJ1002", coordinate, "YAML document is empty");
        }
        if (documents > 1) {
            throw fail("input", "RCJ1002", coordinate, "YAML input must contain exactly one document");
        }
    }

    private static boolean hasExplicitTag(Event event) {
        if (event instanceof ScalarEvent scalar) {
            return scalar.getTag().isPresent();
        }
        if (event instanceof CollectionStartEvent collection) {
            return collection.getTag().isPresent();
        }
        return false;
    }

    private static final class Counter {
        int count;
    }

    private static void checkDataTree(Object value, int depth, Counter counter, String coordinate) {
        counter.count += 1;
        if (counter.count > MAX_NODES) {
            throw fail("input", "RCJ1002", coordinate, "decoded YAML node limit exceeded");
        }
        if (depth > MAX_DEPTH) {
            throw fail("input", "RCJ1002", coordinate, "decoded YAML nesting limit exceeded");
        }
        if (value instanceof Double number && (number.isNaN() || number.isInfinite())) {
            throw fail("input", "RCJ1002", coordinate, "non-finite YAML number");
        }
        if (value instanceof Float number && (number.isNaN() || number.isInfinite())) {
            throw fail("input", "RCJ1002", coordinate, "non-finite YAML number");
        }
        if (value instanceof List<?> items) {
            for (Object item : items) {
                checkDataTree(item, depth + 1, counter, coordinate);
            }
        } else if (value instanceof Map<?, ?> entries) {
            for (Map.Entry<?, ?> entry : entries.entrySet()) {
                if (!(entry.getKey() instanceof String)) {
                    throw fail("input", "RCJ1002", coordinate, "YAML mapping keys must be strings");
                }
                checkDataTree(entry.getValue(), depth + 1, counter, coordinate);
            }
        }
    }

    static ObjectNode load(Path path, boolean model) {
        String coordinate = model ? "model" : "package-target";
        byte[] data;
        try (InputStream stream = Files.newInputStream(path)) {
            data = stream.readNBytes(MAX_BYTES + 1);
        } catch (IOException error) {
            throw fail("input", "RCJ1001", coordinate, "cannot read YAML input: unreadable");
        }
        if (data.length > MAX_BYTES) {
            throw fail("input", "RCJ1001", coordinate, "YAML input exceeds 64 MiB");
        }
        if (model) {
            checkModelBytes(data, coordinate);
        }
        String source = strictDecode(data, coordinate);
        scanEvents(source, model, coordinate);

        Object value;
        try {
            LoadSettings settings =
                    LoadSettings.builder()
                            .setAllowDuplicateKeys(false)
                            .setMaxAliasesForCollections(MAX_ALIASES)
                            .build();
            value = new Load(settings).loadFromString(source);
        } catch (YamlEngineException error) {
            throw fail("input", "RCJ1002", coordinate, firstLine(String.valueOf(error.getMessage())));
        }
        if (value == null) {
            throw fail("input", "RCJ1002", coordinate, "YAML document is empty");
        }
        checkDataTree(value, 0, new Counter(), coordinate);
        if (!(value instanceof Map)) {
            throw fail("input", "RCJ1002", coordinate, "YAML document must be a mapping");
        }
        JsonNode tree = Json.MAPPER.valueToTree(value);
        return (ObjectNode) tree;
    }
}
