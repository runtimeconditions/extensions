package io.runtimeconditions.bindings.emitter;

import com.fasterxml.jackson.databind.JsonNode;
import io.runtimeconditions.bindings.emitter.Plan.DeclarationPlan;
import io.runtimeconditions.bindings.emitter.Plan.EmissionPlan;
import io.runtimeconditions.bindings.emitter.Plan.FieldPlan;
import io.runtimeconditions.bindings.emitter.Plan.ImportedDeclarationPlan;
import io.runtimeconditions.bindings.emitter.Plan.Kind;
import io.runtimeconditions.bindings.emitter.Plan.TypeExpr;
import io.runtimeconditions.bindings.emitter.Plan.TypePlan;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Supplier;

final class Conformance {
    private Conformance() {}

    private static String attempt(Supplier<String> build) {
        try {
            return build.get();
        } catch (DiagnosticError error) {
            return null;
        }
    }

    private static String sample(
            TypeExpr expression, EmissionPlan plan, Map<String, TypePlan> types, List<String> active, String selectedOptional) {
        switch (expression.kind()) {
            case "string", "any":
                return "\"sample\"";
            case "boolean":
                return "true";
            case "integer":
                return "1L";
            case "number":
                return "1.0";
            case "null":
                return "null";
            default:
        }
        String name = plan.names().get(expression.symbolKey());
        TypePlan type = types.get(name);
        if (type.kind() == Kind.ENUM) {
            return type.name() + "." + type.members().get(0).name();
        }
        if (type.kind() == Kind.ALIAS) {
            if (active.contains(name)) {
                throw DiagnosticError.fail("model", "RCJ1012", type.coordinate(), "recursive alias has no finite conformance sample");
            }
            List<String> next = append(active, name);
            TypeExpr inner = type.expression();
            return switch (inner.kind()) {
                case "array" -> {
                    String item = attempt(() -> sample(inner.arguments().get(0), plan, types, next, null));
                    yield "new " + name + "(" + (item == null ? "List.of()" : "List.of(" + item + ")") + ")";
                }
                case "map" -> {
                    String item = attempt(() -> sample(inner.arguments().get(0), plan, types, next, null));
                    yield "new " + name + "(" + (item == null ? "Map.of()" : "Map.of(\"sample\", " + item + ")") + ")";
                }
                default -> {
                    DiagnosticError last = null;
                    for (int index = 0; index < inner.arguments().size(); index += 1) {
                        try {
                            yield "new " + name + ".Variant" + (index + 1) + "(" + sample(inner.arguments().get(index), plan, types, next, null) + ")";
                        } catch (DiagnosticError error) {
                            last = error;
                        }
                    }
                    throw last;
                }
            };
        }
        boolean repeated = active.contains(name);
        List<String> optional = new ArrayList<>();
        for (FieldPlan field : type.fields()) {
            if (!field.required()) {
                optional.add(field.name());
            }
        }
        String selected = selectedOptional != null ? selectedOptional : optional.isEmpty() ? null : optional.get(0);
        List<String> arguments = new ArrayList<>();
        List<String> next = append(active, name);
        for (FieldPlan field : type.fields()) {
            if (!field.required()) {
                if (!repeated && field.name().equals(selected)) {
                    arguments.add(sample(field.typeExpr(), plan, types, next, null));
                } else {
                    arguments.add("null");
                }
                continue;
            }
            if (repeated && List.of("object", "ref", "alias").contains(field.typeExpr().kind())) {
                throw DiagnosticError.fail("model", "RCJ1012", field.coordinate(), "required recursive field has no finite conformance sample");
            }
            arguments.add(sample(field.typeExpr(), plan, types, next, null));
        }
        return "new " + type.name() + "(" + String.join(", ", arguments) + ")";
    }

    private static List<String> append(List<String> list, String item) {
        List<String> result = new ArrayList<>(list);
        result.add(item);
        return result;
    }

    private static List<List<TypePlan>> fieldGroups(List<TypePlan> fields) {
        Map<String, List<TypePlan>> groups = new java.util.TreeMap<>();
        for (TypePlan field : fields) {
            groups.computeIfAbsent(field.schemaCoordinate(), key -> new ArrayList<>()).add(field);
        }
        List<List<TypePlan>> result = new ArrayList<>();
        for (List<TypePlan> group : groups.values()) {
            List<TypePlan> sorted = new ArrayList<>(group);
            sorted.sort(Comparator.comparing(TypePlan::name));
            result.add(sorted);
        }
        if (result.isEmpty()) {
            result.add(List.of());
        }
        return result;
    }

    private static String variableName(Map<String, String> variables, String name) {
        return variables.get(name);
    }

    static String source(EmissionPlan plan, JsonNode model) {
        List<String> lines = new ArrayList<>();
        lines.add("// " + Metadata.header(model));
        lines.add("");
        lines.add("package " + plan.target().javaPackage() + ";");
        lines.add("");
        lines.add("import java.util.List;");
        lines.add("import java.util.Map;");
        lines.add("");
        lines.add("@SuppressWarnings({\"unused\", \"rawtypes\"})");
        lines.add("final class Exercise {");
        lines.add("    private Exercise() {}");
        lines.add("");
        lines.add("    static void exercise() {");
        Map<String, TypePlan> types = new LinkedHashMap<>();
        for (TypePlan item : plan.types()) {
            types.put(item.name(), item);
        }
        Map<String, String> objectVariables = new LinkedHashMap<>();
        Map<String, List<String>> alternates = new LinkedHashMap<>();
        List<TypePlan> ordered = new ArrayList<>(plan.types());
        ordered.sort(Comparator.comparing(TypePlan::name));
        for (int index = 0; index < ordered.size(); index += 1) {
            TypePlan type = ordered.get(index);
            if (type.kind() == Kind.ENUM) {
                for (int member = 0; member < type.members().size(); member += 1) {
                    lines.add("        " + type.name() + " _enum" + index + "_" + member + " = " + type.name() + "." + type.members().get(member).name() + ";");
                }
            } else if (type.kind() == Kind.ALIAS) {
                String key = keyOf(plan, type.name());
                TypeExpr reference = new TypeExpr("alias", List.of(), key);
                if (type.expression().kind().equals("union")) {
                    for (int variant = 0; variant < type.expression().arguments().size(); variant += 1) {
                        int chosen = variant;
                        String value = attempt(() -> sample(type.expression().arguments().get(chosen), plan, types, List.of(type.name()), null));
                        if (value != null) {
                            lines.add("        " + type.name() + " _variant" + index + "_" + variant + " = new " + type.name() + ".Variant" + (variant + 1) + "(" + value + ");");
                        }
                    }
                } else {
                    lines.add("        " + type.name() + " _alias" + index + " = " + sample(reference, plan, types, List.of(), null) + ";");
                }
            } else {
                String key = keyOf(plan, type.name());
                TypeExpr objectExpression = new TypeExpr("object", List.of(), key);
                String variable = "_object" + index;
                objectVariables.put(type.name(), variable);
                lines.add("        " + type.name() + " " + variable + " = " + sample(objectExpression, plan, types, List.of(), null) + ";");
                List<String> optional = new ArrayList<>();
                for (FieldPlan field : type.fields()) {
                    if (!field.required()) {
                        optional.add(field.name());
                    }
                }
                for (int offset = 1; offset < optional.size(); offset += 1) {
                    String alternate = "_objectOptional" + index + "_" + offset;
                    alternates.computeIfAbsent(type.name(), unused -> new ArrayList<>()).add(alternate);
                    lines.add("        " + type.name() + " " + alternate + " = " + sample(objectExpression, plan, types, List.of(), optional.get(offset)) + ";");
                }
            }
        }
        int declarationIndex = 0;
        List<DeclarationPlan> declarations = new ArrayList<>(plan.declarations());
        declarations.sort(Comparator.comparing(DeclarationPlan::function));
        for (DeclarationPlan declaration : declarations) {
            List<TypePlan> fields = new ArrayList<>();
            for (TypePlan item : plan.types()) {
                if (item.isDeclarationField()
                        && item.marker() != null
                        && item.marker().owner().equals(plan.target().rootExtension())
                        && item.marker().protocol().equals(declaration.protocol())) {
                    fields.add(item);
                }
            }
            if (fields.isEmpty()) {
                lines.add("        java.util.function.Function<" + declaration.protocol() + "[], Declaration> _deferred_" + declaration.function() + " = " + Plan.FUNCTIONS_CLASS + "::" + declaration.function() + ";");
                continue;
            }
            for (List<TypePlan> group : fieldGroups(fields)) {
                List<String> arguments = new ArrayList<>();
                for (TypePlan item : group) {
                    arguments.add(variableName(objectVariables, item.name()));
                }
                lines.add("        Declaration _declaration" + declarationIndex + " = " + Plan.FUNCTIONS_CLASS + "." + declaration.function() + "(" + String.join(", ", arguments) + ");");
                declarationIndex += 1;
                for (TypePlan item : group) {
                    for (String alternate : alternates.getOrDefault(item.name(), List.of())) {
                        List<String> replaced = new ArrayList<>();
                        for (TypePlan field : group) {
                            replaced.add(field.name().equals(item.name()) ? alternate : variableName(objectVariables, field.name()));
                        }
                        lines.add("        Declaration _declaration" + declarationIndex + " = " + Plan.FUNCTIONS_CLASS + "." + declaration.function() + "(" + String.join(", ", replaced) + ");");
                        declarationIndex += 1;
                    }
                }
            }
        }
        int importedIndex = 0;
        List<ImportedDeclarationPlan> imported = new ArrayList<>(plan.importedDeclarations());
        imported.sort(Comparator.comparing(ImportedDeclarationPlan::function));
        for (ImportedDeclarationPlan declaration : imported) {
            List<TypePlan> fields = new ArrayList<>();
            for (TypePlan item : plan.types()) {
                if (item.isDeclarationField() && item.marker() != null && item.marker().protocol().equals(declaration.protocol())) {
                    fields.add(item);
                }
            }
            String provider = declaration.providerPackage().javaPackage();
            if (fields.isEmpty()) {
                continue;
            }
            for (List<TypePlan> group : fieldGroups(fields)) {
                List<String> arguments = new ArrayList<>();
                for (TypePlan item : group) {
                    arguments.add(variableName(objectVariables, item.name()));
                }
                lines.add("        " + provider + ".Declaration _imported" + importedIndex + " = " + provider + "." + Plan.FUNCTIONS_CLASS + "." + declaration.function() + "(" + String.join(", ", arguments) + ");");
                importedIndex += 1;
            }
        }
        lines.add("    }");
        lines.add("}");
        lines.add("");
        return String.join("\n", lines);
    }

    private static String keyOf(EmissionPlan plan, String name) {
        for (Map.Entry<String, String> entry : plan.names().entrySet()) {
            if (entry.getValue().equals(name) && entry.getKey().startsWith("type:")) {
                return entry.getKey();
            }
        }
        throw new IllegalStateException(name);
    }
}
