package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.DiagnosticError.fail;

import com.fasterxml.jackson.core.JsonProcessingException;
import java.text.Normalizer;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;

public final class Naming {
    public enum Style {
        PASCAL,
        CAMEL,
        CONSTANT,
        EXACT
    }

    public record Symbol(
            String key,
            String coordinate,
            String source,
            Style style,
            String namespace,
            List<String> parents,
            boolean fixed) {
        public Symbol(String key, String coordinate, String source, Style style) {
            this(key, coordinate, source, style, null, List.of(), false);
        }

        public static Symbol fixed(String key, String coordinate, String source, Style style) {
            return new Symbol(key, coordinate, source, style, null, List.of(), true);
        }

        public static Symbol fixed(
                String key, String coordinate, String source, Style style, String namespace) {
            return new Symbol(key, coordinate, source, style, namespace, List.of(), true);
        }

        public static Symbol scoped(
                String key, String coordinate, String source, Style style, List<String> parents) {
            return new Symbol(key, coordinate, source, style, null, List.copyOf(parents), false);
        }

        public static Symbol inNamespace(
                String key, String coordinate, String source, Style style, String namespace) {
            return new Symbol(key, coordinate, source, style, namespace, List.of(), false);
        }

        String space() {
            return namespace == null ? "module" : namespace;
        }
    }

    private static final Set<String> KEYWORDS =
            Set.of(
                    "_", "abstract", "assert", "boolean", "break", "byte", "case", "catch", "char",
                    "class", "const", "continue", "default", "do", "double", "else", "enum",
                    "extends", "false", "final", "finally", "float", "for", "goto", "if",
                    "implements", "import", "instanceof", "int", "interface", "long", "native",
                    "new", "null", "package", "permits", "private", "protected", "public",
                    "record", "return", "sealed", "short", "static", "strictfp", "super",
                    "switch", "synchronized", "this", "throw", "throws", "transient", "true",
                    "try", "var", "void", "volatile", "while", "yield", "clone", "finalize",
                    "getClass", "hashCode", "notify", "notifyAll", "toString", "wait");

    private static final List<String> HELPERS =
            List.of(
                    "Object", "String", "Long", "Double", "Boolean", "Void", "Objects", "List", "Map",
                    "Override", "Record", "Enum", "Number", "Integer", "Class", "Exception", "Error",
                    "Throwable", "Comparable", "Iterable", "Character", "Byte", "Short", "Float",
                    "System", "Math", "Runnable", "Thread", "Deprecated", "SuppressWarnings",
                    "FunctionalInterface", "SafeVarargs", "CharSequence", "StringBuilder", "Cloneable",
                    "AutoCloseable", "Iterator", "Collection", "Set", "Optional", "Arrays",
                    "Collections");

    private static final Map<String, String> RESERVED = new LinkedHashMap<>();

    static {
        for (String name : HELPERS) {
            RESERVED.put(name, "java-global:" + name);
        }
    }

    private Naming() {}

    static boolean isReservedWord(String name) {
        return KEYWORDS.contains(name);
    }

    private static boolean alphanumeric(int point) {
        if (Character.isLetter(point)) {
            return true;
        }
        int type = Character.getType(point);
        return type == Character.DECIMAL_DIGIT_NUMBER
                || type == Character.LETTER_NUMBER
                || type == Character.OTHER_NUMBER;
    }

    private static boolean upper(int point) {
        return Character.getType(point) == Character.UPPERCASE_LETTER;
    }

    private static boolean lower(int point) {
        return Character.getType(point) == Character.LOWERCASE_LETTER;
    }

    private static boolean digit(int point) {
        return Character.getType(point) == Character.DECIMAL_DIGIT_NUMBER;
    }

    static List<String> tokens(String input) {
        int[] points = Normalizer.normalize(input, Normalizer.Form.NFKC).codePoints().toArray();
        List<String> chunks = new ArrayList<>();
        StringBuilder current = new StringBuilder();
        for (int index = 0; index < points.length; index += 1) {
            int point = points[index];
            if (!alphanumeric(point)) {
                if (current.length() > 0) {
                    chunks.add(current.toString());
                    current.setLength(0);
                }
                continue;
            }
            Integer previous = index > 0 ? points[index - 1] : null;
            Integer following = index + 1 < points.length ? points[index + 1] : null;
            boolean boundary =
                    current.length() > 0
                            && ((upper(point) && previous != null && (lower(previous) || digit(previous)))
                                    || (upper(point)
                                            && previous != null
                                            && upper(previous)
                                            && following != null
                                            && lower(following)));
            if (boundary) {
                chunks.add(current.toString());
                current.setLength(0);
            }
            current.appendCodePoint(point);
        }
        if (current.length() > 0) {
            chunks.add(current.toString());
        }
        return chunks;
    }

    private static String capitalize(String part) {
        int first = part.codePointAt(0);
        String head = new String(Character.toChars(first));
        String rest = part.substring(head.length());
        return head.toUpperCase(Locale.ROOT) + rest.toLowerCase(Locale.ROOT);
    }

    static boolean identifier(String name) {
        if (name.isEmpty()) {
            return false;
        }
        int first = name.codePointAt(0);
        if (!Character.isJavaIdentifierStart(first) || Character.isIdentifierIgnorable(first)) {
            return false;
        }
        for (int index = Character.charCount(first); index < name.length(); ) {
            int point = name.codePointAt(index);
            if (!Character.isJavaIdentifierPart(point) || Character.isIdentifierIgnorable(point)) {
                return false;
            }
            index += Character.charCount(point);
        }
        return !name.equals("_");
    }

    private static String quote(String value) {
        try {
            return Json.MAPPER.writeValueAsString(value);
        } catch (JsonProcessingException error) {
            throw new IllegalStateException(error);
        }
    }

    public static String javaName(String source, Style style, String coordinate) {
        if (style == Style.EXACT) {
            if (!identifier(source)) {
                throw fail(
                        "java-naming",
                        "RCJ2000",
                        coordinate,
                        "invalid Java identifier " + quote(source));
            }
            return source;
        }
        List<String> parts = tokens(source);
        if (parts.isEmpty()) {
            throw fail(
                    "java-naming",
                    "RCJ2000",
                    coordinate,
                    "cannot derive Java identifier from '" + source + "'");
        }
        String name;
        if (style == Style.PASCAL) {
            name = String.join("", parts.stream().map(Naming::capitalize).toList());
        } else if (style == Style.CONSTANT) {
            name = String.join("_", parts.stream().map(part -> part.toUpperCase(Locale.ROOT)).toList());
        } else {
            StringBuilder builder = new StringBuilder();
            for (int index = 0; index < parts.size(); index += 1) {
                builder.append(index == 0 ? parts.get(0).toLowerCase(Locale.ROOT) : capitalize(parts.get(index)));
            }
            name = builder.toString();
        }
        if (Character.getType(name.codePointAt(0)) == Character.DECIMAL_DIGIT_NUMBER) {
            name = (style == Style.CAMEL ? "x" : "X") + name;
        }
        if (KEYWORDS.contains(name)) {
            name += "_";
        }
        if (!identifier(name)) {
            throw fail(
                    "java-naming",
                    "RCJ2000",
                    coordinate,
                    "invalid Java identifier " + quote(name));
        }
        return name;
    }

    private static int compare(String left, String right) {
        return Integer.signum(left.compareTo(right));
    }

    public static Map<String, String> allocateSymbols(String packageCoordinate, List<Symbol> symbols) {
        List<Symbol> ordered = new ArrayList<>(symbols);
        ordered.sort(
                Comparator.comparing(Symbol::space)
                        .thenComparing(Symbol::coordinate)
                        .thenComparing(Symbol::key));
        if (ordered.stream().map(Symbol::key).distinct().count() != ordered.size()) {
            throw fail("java-naming", "RCJ2001", packageCoordinate, "duplicate symbol request key");
        }
        Map<String, Integer> levels = new LinkedHashMap<>();
        for (Symbol item : ordered) {
            levels.put(item.key(), 0);
        }
        int maxDepth = 0;
        for (Symbol item : ordered) {
            maxDepth = Math.max(maxDepth, item.parents().size());
        }
        int rounds = ordered.size() * (maxDepth + 1) + 1;
        for (int round = 0; round < rounds; round += 1) {
            Map<List<String>, List<Symbol>> groups =
                    new TreeMap<>(
                            (a, b) -> {
                                int space = compare(a.get(0), b.get(0));
                                return space != 0 ? space : compare(a.get(1), b.get(1));
                            });
            for (Symbol item : ordered) {
                groups.computeIfAbsent(List.of(item.space(), candidate(item, levels)), unused -> new ArrayList<>())
                        .add(item);
            }
            List<Map.Entry<String, List<Symbol>>> collisions = new ArrayList<>();
            for (Map.Entry<List<String>, List<Symbol>> entry : groups.entrySet()) {
                String space = entry.getKey().get(0);
                String name = entry.getKey().get(1);
                List<Symbol> group = entry.getValue();
                String reserved = space.equals("module") ? RESERVED.get(name) : null;
                if (group.size() > 1 || reserved != null) {
                    List<Symbol> pair = new ArrayList<>(group);
                    pair.sort(Comparator.comparing(Symbol::coordinate));
                    if (reserved != null) {
                        throw fail(
                                "java-naming",
                                "RCJ2001",
                                packageCoordinate,
                                "symbol '" + name + "' conflicts: " + group.get(0).coordinate() + " and " + reserved);
                    }
                    if (group.stream().anyMatch(Symbol::fixed)) {
                        throw fail(
                                "java-naming",
                                "RCJ2001",
                                packageCoordinate,
                                "fixed symbol '"
                                        + name
                                        + "' conflicts: "
                                        + pair.get(0).coordinate()
                                        + " and "
                                        + pair.get(1).coordinate());
                    }
                    collisions.add(Map.entry(name, group));
                }
            }
            if (collisions.isEmpty()) {
                Map<String, String> result = new LinkedHashMap<>();
                for (Symbol item : ordered) {
                    result.put(item.key(), candidate(item, levels));
                }
                return result;
            }
            for (Map.Entry<String, List<Symbol>> collision : collisions) {
                for (Symbol item : collision.getValue()) {
                    if (levels.get(item.key()) == item.parents().size()) {
                        List<Symbol> pair = new ArrayList<>(collision.getValue());
                        pair.sort(Comparator.comparing(Symbol::coordinate));
                        throw fail(
                                "java-naming",
                                "RCJ2001",
                                packageCoordinate,
                                "symbol '"
                                        + collision.getKey()
                                        + "' conflicts after full path: "
                                        + pair.get(0).coordinate()
                                        + " and "
                                        + pair.get(1).coordinate());
                    }
                    levels.put(item.key(), levels.get(item.key()) + 1);
                }
            }
        }
        throw new IllegalStateException("symbol allocation did not converge");
    }

    private static String candidate(Symbol item, Map<String, Integer> levels) {
        List<String> parents = new ArrayList<>(item.parents().subList(0, levels.get(item.key())));
        Collections.reverse(parents);
        parents.add(item.source());
        return javaName(String.join("_", parents), item.style(), item.coordinate());
    }
}
