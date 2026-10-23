package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.DiagnosticError.fail;

import com.fasterxml.jackson.databind.JsonNode;
import io.runtimeconditions.bindings.emitter.Plan.DeclarationPlan;
import io.runtimeconditions.bindings.emitter.Plan.EmissionPlan;
import io.runtimeconditions.bindings.emitter.Plan.FieldPlan;
import io.runtimeconditions.bindings.emitter.Plan.Kind;
import io.runtimeconditions.bindings.emitter.Plan.TypePlan;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.FileAlreadyExistsException;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.Path;
import java.nio.file.StandardOpenOption;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import java.util.TreeSet;
import java.util.stream.Stream;

public final class Source {
    private Source() {}

    public static String sourceRoot(EmissionPlan plan) {
        return "src/main/java/" + plan.target().javaPackage().replace('.', '/') + "/";
    }

    public static String sourcePath(EmissionPlan plan, String name) {
        return sourceRoot(plan) + name + ".java";
    }

    public static List<String> exportedNames(EmissionPlan plan) {
        TreeSet<String> names = new TreeSet<>();
        if (!plan.declarations().isEmpty()) {
            names.add(Plan.DECLARATION_CLASS);
            names.add(Plan.FUNCTIONS_CLASS);
        }
        for (DeclarationPlan item : plan.declarations()) {
            names.add(item.protocol());
        }
        for (TypePlan item : plan.types()) {
            names.add(item.name());
        }
        return new ArrayList<>(names);
    }

    static String literal(String value) {
        StringBuilder builder = new StringBuilder("\"");
        for (int index = 0; index < value.length(); index += 1) {
            char character = value.charAt(index);
            switch (character) {
                case '"' -> builder.append("\\\"");
                case '\\' -> builder.append("\\\\");
                case '\n' -> builder.append("\\n");
                case '\r' -> builder.append("\\r");
                case '\t' -> builder.append("\\t");
                default -> {
                    if (character < 0x20 || character > 0x7e) {
                        builder.append(String.format("\\u%04x", (int) character));
                    } else {
                        builder.append(character);
                    }
                }
            }
        }
        return builder.append('"').toString();
    }

    private static boolean nullable(String annotation) {
        return annotation.equals("Void") || annotation.equals("Object");
    }

    private static String components(List<FieldPlan> fields) {
        List<String> parts = new ArrayList<>();
        for (FieldPlan field : fields) {
            parts.add(field.annotation() + " " + field.name());
        }
        return String.join(", ", parts);
    }

    private static void requireChecks(List<String> lines, List<FieldPlan> fields, String indent, String name) {
        List<FieldPlan> checked = new ArrayList<>();
        for (FieldPlan field : fields) {
            if (field.required() && !nullable(field.annotation())) {
                checked.add(field);
            }
        }
        if (checked.isEmpty()) {
            return;
        }
        lines.add("");
        lines.add(indent + "    public " + name + " {");
        for (FieldPlan field : checked) {
            lines.add(indent + "        Objects.requireNonNull(" + field.name() + ", " + literal(field.name()) + ");");
        }
        lines.add(indent + "    }");
    }

    private static List<String> recordBody(
            String name, List<FieldPlan> fields, String implement, String markerMethod, String indent) {
        List<String> lines = new ArrayList<>();
        String modifier = indent.isEmpty() ? "public " : "";
        String declaration = indent + modifier + "record " + name + "(" + components(fields) + ")" + implement;
        boolean checks = fields.stream().anyMatch(field -> field.required() && !nullable(field.annotation()));
        if (!checks && markerMethod == null) {
            lines.add(declaration + " {}");
            return lines;
        }
        lines.add(declaration + " {");
        List<String> body = new ArrayList<>();
        requireChecks(body, fields, indent, name);
        if (markerMethod != null) {
            body.add("");
            body.add(indent + "    @Override");
            body.add(indent + "    public void " + markerMethod + "() {}");
        }
        if (!body.isEmpty() && body.get(0).isEmpty()) {
            body.remove(0);
        }
        lines.addAll(body);
        lines.add(indent + "}");
        return lines;
    }

    private static String file(EmissionPlan plan, JsonNode model, String name, List<String> body, List<String> extraImports) {
        TreeSet<String> imports = new TreeSet<>(extraImports);
        String text = String.join("\n", body);
        if (text.contains("List<")) {
            imports.add("java.util.List");
        }
        if (text.contains("Map<")) {
            imports.add("java.util.Map");
        }
        if (text.contains("Objects.")) {
            imports.add("java.util.Objects");
        }
        List<String> lines = new ArrayList<>();
        lines.add("// " + Metadata.header(model));
        lines.add("");
        lines.add("package " + plan.target().javaPackage() + ";");
        lines.add("");
        if (!imports.isEmpty()) {
            for (String item : imports) {
                lines.add("import " + item + ";");
            }
            lines.add("");
        }
        lines.addAll(body);
        lines.add("");
        return String.join("\n", lines);
    }

    private static String implementsClause(TypePlan item, List<String> imports) {
        if (item.marker() == null) {
            return "";
        }
        if (item.marker().providerPackage() != null) {
            imports.add(item.marker().providerPackage().javaPackage() + "." + item.marker().protocol());
        }
        return " implements " + item.marker().protocol();
    }

    private static String renderType(EmissionPlan plan, JsonNode model, TypePlan item) {
        List<String> imports = new ArrayList<>();
        List<String> body = new ArrayList<>();
        if (item.kind() == Kind.ENUM) {
            body.add("public enum " + item.name() + " {");
            for (int index = 0; index < item.members().size(); index += 1) {
                Plan.Member member = item.members().get(index);
                body.add(
                        "    "
                                + member.name()
                                + "("
                                + literal(member.value())
                                + ")"
                                + (index + 1 < item.members().size() ? "," : ";"));
            }
            body.add("");
            body.add("    private final String value;");
            body.add("");
            body.add("    " + item.name() + "(String value) {");
            body.add("        this.value = value;");
            body.add("    }");
            body.add("");
            body.add("    public String value() {");
            body.add("        return value;");
            body.add("    }");
            body.add("}");
        } else if (item.kind() == Kind.ALIAS) {
            String kind = item.expression().kind();
            if (kind.equals("union")) {
                body.add("public sealed interface " + item.name() + " {");
                for (int index = 0; index < item.expression().arguments().size(); index += 1) {
                    String annotation = Plan.annotate(item.expression().arguments().get(index), plan.names());
                    List<FieldPlan> single =
                            List.of(new FieldPlan("", "value", "value", item.expression().arguments().get(index), annotation, true));
                    if (index > 0) {
                        body.add("");
                    }
                    String variant = "Variant" + (index + 1);
                    body.addAll(recordBody(variant, single, " implements " + item.name(), null, "    "));
                }
                body.add("}");
            } else {
                String annotation = Plan.annotate(item.expression(), plan.names());
                List<FieldPlan> single = List.of(new FieldPlan("", "value", "value", item.expression(), annotation, true));
                body.addAll(recordBody(item.name(), single, "", null, ""));
            }
        } else {
            String implement = implementsClause(item, imports);
            body.addAll(
                    recordBody(
                            item.name(),
                            item.fields(),
                            implement,
                            item.marker() == null ? null : item.marker().method(),
                            ""));
        }
        return file(plan, model, item.name(), body, imports);
    }

    private static String renderProtocol(EmissionPlan plan, JsonNode model, DeclarationPlan declaration) {
        List<String> body =
                List.of(
                        "public interface " + declaration.protocol() + " {",
                        "    void " + declaration.method() + "();",
                        "}");
        return file(plan, model, declaration.protocol(), body, List.of());
    }

    private static String renderDeclaration(EmissionPlan plan, JsonNode model) {
        List<String> body =
                List.of("public final class " + Plan.DECLARATION_CLASS + " {", "    Declaration() {}", "}");
        return file(plan, model, Plan.DECLARATION_CLASS, body, List.of());
    }

    private static String renderFunctions(EmissionPlan plan, JsonNode model) {
        List<String> body = new ArrayList<>();
        body.add("public final class " + Plan.FUNCTIONS_CLASS + " {");
        body.add("    private " + Plan.FUNCTIONS_CLASS + "() {}");
        List<DeclarationPlan> ordered = new ArrayList<>(plan.declarations());
        ordered.sort(Comparator.comparing(DeclarationPlan::function));
        for (DeclarationPlan declaration : ordered) {
            body.add("");
            body.add(
                    "    public static "
                            + Plan.DECLARATION_CLASS
                            + " "
                            + declaration.function()
                            + "("
                            + declaration.protocol()
                            + "... fields) {");
            body.add("        Objects.requireNonNull(fields, \"fields\");");
            body.add("        return new " + Plan.DECLARATION_CLASS + "();");
            body.add("    }");
        }
        body.add("}");
        return file(plan, model, Plan.FUNCTIONS_CLASS, body, List.of());
    }

    public static Map<String, String> renderSources(EmissionPlan plan, JsonNode model) {
        if (!model.path("metadata").path("semanticSha256").asText().equals(plan.modelDigest())
                || !model.path("rootExtension").path("id").asText().equals(plan.target().rootExtension())) {
            throw fail("model", "RCJ1003", "model", "emission plan does not match supplied model");
        }
        Map<String, String> sources = new TreeMap<>();
        if (!plan.declarations().isEmpty()) {
            sources.put(sourcePath(plan, Plan.DECLARATION_CLASS), renderDeclaration(plan, model));
            sources.put(sourcePath(plan, Plan.FUNCTIONS_CLASS), renderFunctions(plan, model));
        }
        for (DeclarationPlan declaration : plan.declarations()) {
            sources.put(sourcePath(plan, declaration.protocol()), renderProtocol(plan, model, declaration));
        }
        for (TypePlan item : plan.types()) {
            sources.put(sourcePath(plan, item.name()), renderType(plan, model, item));
        }
        return sources;
    }

    public static Map<String, String> renderPackage(EmissionPlan plan, JsonNode model) {
        Map<String, String> files = new TreeMap<>(renderSources(plan, model));
        files.putAll(Metadata.renderResources(plan, model));
        return files;
    }

    public static List<String> writeFiles(Map<String, String> files, EmissionPlan plan, Path output) {
        String key = plan.target().packageKey();
        boolean occupied;
        try {
            occupied =
                    Files.exists(output, LinkOption.NOFOLLOW_LINKS)
                            && (Files.isSymbolicLink(output)
                                    || !Files.isDirectory(output, LinkOption.NOFOLLOW_LINKS)
                                    || hasEntries(output));
        } catch (IOException error) {
            throw fail("output", "RCJ3002", key, "cannot inspect output directory");
        }
        if (occupied) {
            throw fail("output", "RCJ3001", key, "output directory must be new or empty and not a symbolic link");
        }
        try {
            Files.createDirectories(output);
            for (Map.Entry<String, String> entry : new TreeMap<>(files).entrySet()) {
                Path destination = output.resolve(entry.getKey());
                Files.createDirectories(destination.getParent());
                Files.writeString(destination, entry.getValue(), StandardCharsets.UTF_8, StandardOpenOption.CREATE_NEW);
            }
        } catch (FileAlreadyExistsException error) {
            throw fail("output", "RCJ3001", key, "output directory must be new or empty and not a symbolic link");
        } catch (IOException error) {
            throw fail("output", "RCJ3002", key, "cannot write generated package files");
        }
        return new ArrayList<>(new TreeMap<>(files).keySet());
    }

    private static boolean hasEntries(Path directory) throws IOException {
        try (Stream<Path> entries = Files.list(directory)) {
            return entries.findAny().isPresent();
        }
    }

    public static List<String> emitPackage(EmissionPlan plan, JsonNode model, Path output) {
        return writeFiles(renderPackage(plan, model), plan, output);
    }
}
