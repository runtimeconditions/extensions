package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.DiagnosticError.fail;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Reads the files under {@code templates/} and fills their {@code {{name}}} placeholders. */
public final class Templates {
    private static final Pattern PLACEHOLDER = Pattern.compile("\\{\\{(\\w+)}}");
    private static final Pattern LINE = Pattern.compile("(?m)^[ \\t]*\\{\\{(\\w+)}}\\n");

    private Templates() {}

    public static String read(String name) {
        try (InputStream stream = Templates.class.getResourceAsStream("/templates/" + name)) {
            if (stream == null) {
                throw fail("toolchain", "RCJ4001", name, "template '" + name + "' cannot be read");
            }
            return new String(stream.readAllBytes(), StandardCharsets.UTF_8);
        } catch (IOException error) {
            throw fail("toolchain", "RCJ4001", name, "template '" + name + "' cannot be read");
        }
    }

    private static String lookup(Map<String, String> variables, String key, String template) {
        String value = variables.get(key);
        if (value == null) {
            throw fail("toolchain", "RCJ4001", template, "template placeholder '{{" + key + "}}' has no value");
        }
        return value;
    }

    /**
     * Replaces {@code {{name}}} in {@code text}. A placeholder alone on its line is replaced together with
     * that line, so its value supplies whole lines and an empty value leaves none behind.
     */
    public static String fill(String template, String text, Map<String, String> variables) {
        String lines = LINE.matcher(text).replaceAll(match -> Matcher.quoteReplacement(lookup(variables, match.group(1), template)));
        return PLACEHOLDER.matcher(lines).replaceAll(match -> Matcher.quoteReplacement(lookup(variables, match.group(1), template)));
    }
}
