package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.DiagnosticError.fail;

import com.fasterxml.jackson.databind.JsonNode;
import io.runtimeconditions.bindings.emitter.Naming.Style;
import io.runtimeconditions.bindings.emitter.Naming.Symbol;
import io.runtimeconditions.bindings.emitter.Target.Dependency;
import io.runtimeconditions.bindings.emitter.Target.PackageTarget;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.TreeSet;

public final class Plan {
    public record TypeExpr(String kind, List<TypeExpr> arguments, String symbolKey) {}

    public record FieldPlan(
            String coordinate,
            String sourceName,
            String name,
            TypeExpr typeExpr,
            String annotation,
            boolean required) {}

    public record ProviderPackage(String coordinate, String javaPackage) {}

    public record MarkerReference(
            String owner, String protocol, String method, ProviderPackage providerPackage) {}

    public enum Kind {
        OBJECT,
        ALIAS,
        ENUM
    }

    public record Member(String name, String value) {}

    public record TypePlan(
            String coordinate,
            String name,
            Kind kind,
            List<FieldPlan> fields,
            TypeExpr expression,
            List<Member> members,
            MarkerReference marker,
            boolean isDeclarationField,
            String schemaCoordinate,
            String sourceName) {}

    public record DeclarationPlan(
            String coordinate, String kind, String function, String protocol, String method) {}

    public record ImportedDeclarationPlan(
            String coordinate,
            String owner,
            String kind,
            String function,
            String protocol,
            String method,
            ProviderPackage providerPackage) {}

    public record EmissionPlan(
            PackageTarget target,
            Map<String, String> names,
            List<DeclarationPlan> declarations,
            List<ImportedDeclarationPlan> importedDeclarations,
            List<TypePlan> types,
            boolean usesAny,
            String modelDigest) {}

    public static final String DECLARATION_CLASS = "Declaration";
    public static final String FUNCTIONS_CLASS = "Bindings";

    private Plan() {}

    private static TypeExpr expr(String kind) {
        return new TypeExpr(kind, List.of(), "");
    }

    private static TypeExpr expr(String kind, List<TypeExpr> arguments) {
        return new TypeExpr(kind, arguments, "");
    }

    private static TypeExpr named(String kind, String key) {
        return new TypeExpr(kind, List.of(), key);
    }

    /**
     * Replaces a reference to a definition that has no type of its own (a scalar, {@code any}, or another
     * reference) with that definition's expression, following chains. Objects, enums, and collections keep
     * their named reference.
     */
    static TypeExpr resolve(TypeExpr expression, Map<String, TypeExpr> definitions, List<String> active) {
        if (expression.kind().equals("ref")) {
            String key = expression.symbolKey();
            TypeExpr definition = definitions.get(key);
            if (definition == null) {
                throw fail("model", "RCJ1012", key, "reference to a definition that was not planned");
            }
            if (definition.symbolKey().equals(key)) {
                return expression;
            }
            if (active.contains(key)) {
                throw fail("model", "RCJ1012", key, "definition refers to itself without a structure");
            }
            List<String> next = new ArrayList<>(active);
            next.add(key);
            return resolve(definition, definitions, next);
        }
        if (expression.arguments().isEmpty()) {
            return expression;
        }
        List<TypeExpr> arguments = new ArrayList<>();
        for (TypeExpr argument : expression.arguments()) {
            arguments.add(resolve(argument, definitions, active));
        }
        return new TypeExpr(expression.kind(), List.copyOf(arguments), expression.symbolKey());
    }

    public static String annotate(TypeExpr expression, Map<String, String> names) {
        return switch (expression.kind()) {
            case "string" -> "String";
            case "boolean" -> "Boolean";
            case "integer" -> "Long";
            case "number" -> "Double";
            case "null" -> "Void";
            case "any" -> "Object";
            case "object", "enum", "ref", "alias" -> names.get(expression.symbolKey());
            case "array" -> "List<" + annotate(expression.arguments().get(0), names) + ">";
            case "map" -> "Map<String, " + annotate(expression.arguments().get(0), names) + ">";
            default -> throw new IllegalStateException("unplanned type kind " + expression.kind());
        };
    }

    public static String markerMethod(String owner, String coordinate) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            digest.update(owner.getBytes(StandardCharsets.UTF_8));
            digest.update((byte) 0);
            digest.update(coordinate.getBytes(StandardCharsets.UTF_8));
            StringBuilder hex = new StringBuilder();
            for (byte value : digest.digest()) {
                hex.append(String.format("%02x", value));
            }
            return "rcMarker_" + hex.substring(0, 16);
        } catch (NoSuchAlgorithmException error) {
            throw new IllegalStateException(error);
        }
    }

    private static Set<String> directDependencies(JsonNode model) {
        String root = model.path("rootExtension").path("id").asText();
        for (JsonNode extension : Json.list(model.path("extensions"))) {
            if (extension.path("id").asText().equals(root)) {
                Set<String> result = new LinkedHashSet<>();
                for (JsonNode item : Json.list(extension.path("dependencies"))) {
                    result.add(item.asText());
                }
                return result;
            }
        }
        throw fail("model", "RCJ1011", root, "root extension is absent from resolved extension closure");
    }

    private static ProviderPackage providerPackage(PackageTarget target, String owner) {
        for (Dependency dependency : target.dependencies()) {
            if (dependency.extension().equals(owner)) {
                return new ProviderPackage(dependency.coordinate(), dependency.javaPackage());
            }
        }
        throw fail(
                "package-config",
                "RCJ1011",
                owner,
                "imported declaration owner has no entry in the target dependencies",
                "/dependencies");
    }

    static String location(JsonNode item) {
        JsonNode provenance = item.path("provenance");
        return provenance.path("coordinate").asText() + (provenance.has("jsonPointer") ? provenance.path("jsonPointer").asText() : "");
    }

    private record RawField(
            String key, String coordinate, String sourceName, TypeExpr typeExpr, boolean required) {}

    private record RawType(
            Kind kind,
            JsonNode shape,
            TypeExpr expression,
            List<RawField> fields,
            List<String[]> members,
            MarkerReference marker) {}

    private record ImportedRaw(
            String coordinate,
            String owner,
            String kind,
            String functionKey,
            String protocolKey,
            String method,
            ProviderPackage providerPackage) {}

    private static final class Builder {
        final JsonNode model;
        final PackageTarget target;
        final String root;
        final List<Symbol> symbols = new ArrayList<>();
        final Map<String, RawType> rawTypes = new TreeMap<>();
        final Map<String, String> typeSources = new LinkedHashMap<>();
        final Map<String, String> declarationFieldSchemas = new LinkedHashMap<>();
        boolean anyUsed;
        final Map<List<String>, String> definitionKeys = new LinkedHashMap<>();
        final Set<String> referencedDefinitions = new LinkedHashSet<>();
        final Map<String, TypeExpr> definitionExpressions = new LinkedHashMap<>();

        Builder(JsonNode model, PackageTarget target) {
            this.model = model;
            this.target = target;
            this.root = model.path("rootExtension").path("id").asText();
        }

        TypeExpr registerShape(
                JsonNode shape,
                String key,
                String source,
                List<String> parents,
                MarkerReference marker,
                String exactSource) {
            String coordinate = location(shape);
            String kind = shape.path("kind").asText();
            String sourceName = exactSource != null ? exactSource : source;
            if (kind.equals("any")) {
                anyUsed = true;
                return expr("any");
            }
            if (kind.equals("scalar")) {
                List<JsonNode> values = Json.list(shape.path("values"));
                if (!values.isEmpty() && shape.path("scalar").asText().equals("string")) {
                    for (JsonNode entry : values) {
                        if (!entry.path("value").isTextual()) {
                            throw fail("model", "RCJ1012", coordinate, "string domain contains non-string value");
                        }
                    }
                    symbols.add(Symbol.scoped(key, coordinate, source, Style.PASCAL, parents));
                    List<String[]> members = new ArrayList<>();
                    for (int index = 0; index < values.size(); index += 1) {
                        String value = values.get(index).path("value").asText();
                        String memberKey = "member:" + key + ":" + index;
                        symbols.add(
                                Symbol.inNamespace(
                                        memberKey,
                                        coordinate + "#value:" + value,
                                        value,
                                        Style.CONSTANT,
                                        "enum:" + key));
                        members.add(new String[] {memberKey, value});
                    }
                    rawTypes.put(key, new RawType(Kind.ENUM, shape, null, List.of(), members, marker));
                    typeSources.put(key, sourceName);
                    return named("enum", key);
                }
                return expr(shape.path("scalar").asText());
            }
            if (kind.equals("ref")) {
                String reference = shape.path("ref").asText().replaceFirst("^#", "");
                String referenceKey =
                        definitionKeys.get(List.of(shape.path("provenance").path("coordinate").asText(), reference));
                if (referenceKey == null) {
                    throw fail("model", "RCJ1012", coordinate, "unresolved reference '" + shape.path("ref").asText() + "'");
                }
                referencedDefinitions.add(referenceKey);
                return named("ref", referenceKey);
            }
            if (kind.equals("object")) {
                symbols.add(Symbol.scoped(key, coordinate, source, Style.PASCAL, parents));
                if (marker != null) {
                    symbols.add(
                            Symbol.fixed(
                                    "method:" + key,
                                    marker.owner(),
                                    marker.method(),
                                    Style.EXACT,
                                    "fields:" + key));
                }
                List<RawField> rawFields = new ArrayList<>();
                Set<String> required = new TreeSet<>();
                for (JsonNode name : Json.list(shape.path("required"))) {
                    required.add(name.asText());
                }
                List<JsonNode> properties = Json.list(shape.path("properties"));
                Set<String> propertyNames = new TreeSet<>();
                for (JsonNode property : properties) {
                    propertyNames.add(property.path("name").asText());
                }
                List<String> missing = new ArrayList<>();
                for (String name : required) {
                    if (!propertyNames.contains(name)) {
                        missing.add(name);
                    }
                }
                if (!missing.isEmpty()) {
                    throw fail(
                            "model",
                            "RCJ1012",
                            coordinate,
                            "required properties are missing: " + jsonList(missing));
                }
                if (propertyNames.size() != properties.size()) {
                    throw fail("model", "RCJ1012", coordinate, "duplicate object property");
                }
                for (JsonNode property : properties) {
                    String name = property.path("name").asText();
                    String propertyCoordinate = location(property);
                    String fieldKey = "field:" + key + ":" + name;
                    symbols.add(Symbol.inNamespace(fieldKey, propertyCoordinate, name, Style.CAMEL, "fields:" + key));
                    List<String> childParents = new ArrayList<>();
                    childParents.add(source);
                    childParents.addAll(parents);
                    TypeExpr expression =
                            registerShape(property.path("shape"), "type:" + key + ":" + name, name, childParents, null, null);
                    rawFields.add(
                            new RawField(
                                    fieldKey,
                                    propertyCoordinate,
                                    name,
                                    expression,
                                    required.contains(name) || property.path("required").asBoolean(false)));
                }
                rawTypes.put(key, new RawType(Kind.OBJECT, shape, null, rawFields, List.of(), marker));
                typeSources.put(key, sourceName);
                return named("object", key);
            }
            if (kind.equals("array") || kind.equals("map") || kind.equals("union")) {
                List<JsonNode> children;
                if (kind.equals("union")) {
                    children = Json.list(shape.path("variants"));
                    if (children.isEmpty()) {
                        throw fail("model", "RCJ1012", coordinate, "union has no variants");
                    }
                } else {
                    children = List.of(shape.path(kind.equals("array") ? "items" : "mapValues"));
                }
                List<TypeExpr> expressions = new ArrayList<>();
                for (int index = 0; index < children.size(); index += 1) {
                    String suffix =
                            kind.equals("array") ? "Item" : kind.equals("map") ? "Value" : "Variant" + (index + 1);
                    String base = kind.equals("array") ? source.replaceFirst(" Value$", "") : source;
                    expressions.add(
                            registerShape(
                                    children.get(index),
                                    "type:" + key + ":" + suffix,
                                    base + " " + suffix,
                                    parents,
                                    null,
                                    sourceName));
                }
                symbols.add(Symbol.scoped(key, coordinate, source, Style.PASCAL, parents));
                rawTypes.put(key, new RawType(Kind.ALIAS, shape, expr(kind, expressions), List.of(), List.of(), marker));
                typeSources.put(key, sourceName);
                return named("alias", key);
            }
            throw fail("model", "RCJ1010", coordinate, "unknown structural node '" + kind + "'");
        }
    }

    private static String jsonList(List<String> values) {
        try {
            return Json.MAPPER.writeValueAsString(values);
        } catch (com.fasterxml.jackson.core.JsonProcessingException error) {
            throw new IllegalStateException(error);
        }
    }

    private static List<String> list(String... values) {
        return List.of(values);
    }

    private static List<String> pendingReferenced(Set<String> referenced, Map<String, Object[]> pending) {
        List<String> result = new ArrayList<>();
        for (String item : referenced) {
            if (pending.containsKey(item)) {
                result.add(item);
            }
        }
        result.sort(Comparator.naturalOrder());
        return result;
    }

    public static EmissionPlan build(JsonNode model, PackageTarget target) {
        String root = model.path("rootExtension").path("id").asText();
        String pkg = target.packageKey();
        if (!root.equals(target.rootExtension())) {
            throw fail(
                    "package-config",
                    "RCJ1009",
                    pkg,
                    "target root '" + target.rootExtension() + "' differs from model root '" + root + "'",
                    "/rootExtension");
        }
        Set<String> direct = directDependencies(model);
        Set<String> configured = new TreeSet<>();
        for (Dependency dependency : target.dependencies()) {
            configured.add(dependency.extension());
        }
        Set<String> owners = new TreeSet<>();
        for (JsonNode item : Json.list(model.path("vocabulary").path("importedDeclarations"))) {
            owners.add(item.path("owner").asText());
        }
        Set<String> allowed = new TreeSet<>(direct);
        allowed.addAll(owners);
        if (!configured.containsAll(direct) || !allowed.containsAll(configured)) {
            throw fail(
                    "package-config",
                    "RCJ1009",
                    pkg,
                    "dependencies must be the direct dependencies plus the owners of imported declarations: model="
                            + jsonList(new ArrayList<>(allowed))
                            + ", target="
                            + jsonList(new ArrayList<>(configured)),
                    "/dependencies");
        }
        Set<String> extensionIds = new HashSet<>();
        for (JsonNode item : Json.list(model.path("extensions"))) {
            extensionIds.add(item.path("id").asText());
        }
        if (!extensionIds.containsAll(configured)) {
            throw fail("model", "RCJ1011", root, "target dependency is absent from resolved closure");
        }

        Builder builder = new Builder(model, target);
        JsonNode vocabulary = model.path("vocabulary");
        List<String[]> declarations = new ArrayList<>();
        List<ImportedRaw> imported = new ArrayList<>();
        Map<String, ImportedRaw> importedKinds = new LinkedHashMap<>();
        Map<String, String> ownedKinds = new LinkedHashMap<>();

        for (JsonNode item : Json.list(vocabulary.path("ownedDeclarations"))) {
            String coordinate = item.path("coordinate").asText();
            String kind = item.path("kind").asText();
            if (!item.path("owner").asText().equals(root)) {
                throw fail("model", "RCJ1011", coordinate, "owned declaration has a foreign owner");
            }
            if (ownedKinds.containsKey(kind)) {
                throw fail("model", "RCJ1011", coordinate, "duplicate owned kind '" + kind + "'");
            }
            ownedKinds.put(kind, coordinate);
            builder.symbols.add(Symbol.fixed("function:" + coordinate, coordinate, kind, Style.CAMEL));
            builder.symbols.add(Symbol.fixed("protocol:" + coordinate, coordinate, kind + " Field", Style.PASCAL));
            declarations.add(new String[] {coordinate, "function:" + coordinate, "protocol:" + coordinate});
        }
        if (!declarations.isEmpty()) {
            builder.symbols.add(Symbol.fixed("builtin:Declaration", "java:Declaration", DECLARATION_CLASS, Style.PASCAL));
            builder.symbols.add(Symbol.fixed("builtin:Bindings", "java:Bindings", FUNCTIONS_CLASS, Style.PASCAL));
        }
        for (JsonNode item : Json.list(vocabulary.path("importedDeclarations"))) {
            String owner = item.path("owner").asText();
            String coordinate = item.path("coordinate").asText();
            if (owner.equals(root) || !extensionIds.contains(owner)) {
                throw fail("model", "RCJ1011", coordinate, "invalid imported declaration owner");
            }
            String kind = item.path("kind").asText();
            String functionKey = "import:function:" + coordinate;
            String protocolKey = "import:protocol:" + coordinate;
            builder.symbols.add(Symbol.fixed(functionKey, coordinate, kind, Style.CAMEL));
            builder.symbols.add(Symbol.fixed(protocolKey, coordinate, kind + " Field", Style.PASCAL));
            ImportedRaw plan =
                    new ImportedRaw(
                            coordinate,
                            owner,
                            kind,
                            functionKey,
                            protocolKey,
                            markerMethod(owner, coordinate),
                            providerPackage(target, owner));
            imported.add(plan);
            if (importedKinds.containsKey(kind)) {
                throw fail("model", "RCJ1011", coordinate, "duplicate imported kind");
            }
            importedKinds.put(kind, plan);
        }

        Map<String, Object[]> pendingDefinitions = new TreeMap<>();
        Set<List<String>> ownedFieldRoots = new HashSet<>();
        for (JsonNode item : Json.list(vocabulary.path("conditionFields"))) {
            if (item.path("owner").asText().equals(root)) {
                ownedFieldRoots.add(
                        list(
                                item.path("kind").asText(),
                                item.path("interfaceType").asText(""),
                                item.path("segments").path(0).path("name").asText()));
            }
        }
        Map<List<String>, JsonNode> scopeProjections = new LinkedHashMap<>();
        for (JsonNode item : Json.list(model.path("scopes"))) {
            scopeProjections.put(
                    list(item.path("kind").asText(), item.path("interfaceType").asText("")),
                    item.path("projection"));
        }
        for (JsonNode schema : Json.list(model.path("schemas"))) {
            for (JsonNode definition : Json.list(schema.path("definitions"))) {
                builder.definitionKeys.put(
                        list(schema.path("coordinate").asText(), definition.path("jsonPointer").asText()),
                        "type:" + schema.path("coordinate").asText() + ":" + definition.path("jsonPointer").asText());
            }
        }

        for (JsonNode schema : Json.list(model.path("schemas"))) {
            if (!schema.path("owner").asText().equals(root)) {
                continue;
            }
            String schemaCoordinate = schema.path("coordinate").asText();
            String kind = schema.has("kind") ? schema.path("kind").asText() : schema.path("id").asText();
            String interfaceType = schema.has("interfaceType") ? schema.path("interfaceType").asText() : null;
            List<String> scope = new ArrayList<>();
            if (interfaceType != null && !interfaceType.isEmpty()) {
                scope.add(interfaceType);
            }
            if (!kind.isEmpty()) {
                scope.add(kind);
            }
            MarkerReference marker = null;
            if (ownedKinds.containsKey(kind)) {
                String declarationCoordinate = ownedKinds.get(kind);
                marker =
                        new MarkerReference(
                                root,
                                "protocol:" + declarationCoordinate,
                                markerMethod(root, declarationCoordinate),
                                null);
            } else if (importedKinds.containsKey(kind)) {
                ImportedRaw item = importedKinds.get(kind);
                marker = new MarkerReference(item.owner(), item.protocolKey(), item.method(), item.providerPackage());
            }
            JsonNode projection = schema.path("projection");
            if (!projection.path("kind").asText().equals("object")) {
                throw fail("model", "RCJ1012", schemaCoordinate, "root condition projection must be an object");
            }
            List<JsonNode> projectionProperties = Json.list(projection.path("properties"));
            for (JsonNode property : projectionProperties) {
                String propertyName = property.path("name").asText();
                boolean isInterface = propertyName.equals("interface") && interfaceType != null && !interfaceType.isEmpty();
                String fieldSource = isInterface ? interfaceType : propertyName;
                List<String> fieldParents = new ArrayList<>();
                if (isInterface) {
                    fieldParents.add(propertyName);
                }
                fieldParents.addAll(scope);
                String key = "type:field:" + schemaCoordinate + ":" + propertyName;
                builder.declarationFieldSchemas.put(key, schemaCoordinate);
                JsonNode shape = property.path("shape");
                if (ownedFieldRoots.contains(list(kind, interfaceType == null ? "" : interfaceType, propertyName))) {
                    JsonNode scopeProjection =
                            scopeProjections.getOrDefault(
                                    list(kind, interfaceType == null ? "" : interfaceType),
                                    Json.MAPPER.createObjectNode());
                    JsonNode projected = null;
                    for (JsonNode candidate : Json.list(scopeProjection.path("properties"))) {
                        if (candidate.path("name").asText().equals(propertyName)) {
                            projected = candidate;
                            break;
                        }
                    }
                    if (projected == null) {
                        throw fail(
                                "model",
                                "RCJ1012",
                                schemaCoordinate,
                                "condition field '" + propertyName + "' is absent from its scope projection");
                    }
                    shape = projected.path("shape");
                }
                if (shape.path("kind").asText().equals("object")) {
                    builder.registerShape(shape, key, fieldSource, fieldParents, marker, null);
                    continue;
                }
                if (marker == null) {
                    throw fail(
                            "model",
                            "RCJ1011",
                            schemaCoordinate,
                            "field '" + propertyName + "' has no declaration contract");
                }
                String coordinate = location(property);
                builder.symbols.add(Symbol.scoped(key, coordinate, fieldSource, Style.PASCAL, fieldParents));
                builder.symbols.add(
                        Symbol.fixed("method:" + key, marker.owner(), marker.method(), Style.EXACT, "fields:" + key));
                String valueKey = "field:" + key + ":value";
                builder.symbols.add(Symbol.fixed(valueKey, coordinate, "value", Style.CAMEL, "fields:" + key));
                TypeExpr fieldExpression =
                        builder.registerShape(shape, "type:" + key + ":value", fieldSource + " Value", fieldParents, null, null);
                builder.rawTypes.put(
                        key,
                        new RawType(
                                Kind.OBJECT,
                                shape,
                                null,
                                List.of(new RawField(valueKey, coordinate, "value", fieldExpression, true)),
                                List.of(),
                                marker));
                builder.typeSources.put(key, fieldSource);
            }
            for (JsonNode definition : Json.list(schema.path("definitions"))) {
                String definitionKey =
                        builder.definitionKeys.get(list(schemaCoordinate, definition.path("jsonPointer").asText()));
                pendingDefinitions.put(definitionKey, new Object[] {definition, scope, schemaCoordinate});
                if (projectionProperties.isEmpty()) {
                    builder.referencedDefinitions.add(definitionKey);
                }
            }
        }

        // Planning a definition can reference more definitions. Each pass removes the definitions it
        // plans from pendingDefinitions, so the set only shrinks and the loop ends when none is left.
        List<String> required = pendingReferenced(builder.referencedDefinitions, pendingDefinitions);
        while (!required.isEmpty()) {
            for (String definitionKey : required) {
                Object[] pending = pendingDefinitions.remove(definitionKey);
                JsonNode definition = (JsonNode) pending[0];
                @SuppressWarnings("unchecked")
                List<String> scope = (List<String>) pending[1];
                builder.definitionExpressions.put(
                        definitionKey,
                        builder.registerShape(
                                definition.path("shape"), definitionKey, definition.path("name").asText(), scope, null, null));
            }
            required = pendingReferenced(builder.referencedDefinitions, pendingDefinitions);
        }

        Map<String, String> names = Naming.allocateSymbols(pkg, builder.symbols);
        List<TypePlan> types = new ArrayList<>();
        for (Map.Entry<String, RawType> entry : builder.rawTypes.entrySet()) {
            String key = entry.getKey();
            RawType raw = entry.getValue();
            List<FieldPlan> fields = new ArrayList<>();
            for (RawField field : raw.fields()) {
                TypeExpr resolved = resolve(field.typeExpr(), builder.definitionExpressions, new ArrayList<>());
                fields.add(
                        new FieldPlan(
                                field.coordinate(),
                                field.sourceName(),
                                names.get(field.key()),
                                resolved,
                                annotate(resolved, names),
                                field.required()));
            }
            fields.sort(
                    Comparator.comparing((FieldPlan field) -> !field.required())
                            .thenComparing(FieldPlan::name)
                            .thenComparing(FieldPlan::coordinate));
            List<Member> members = new ArrayList<>();
            for (String[] member : raw.members()) {
                members.add(new Member(names.get(member[0]), member[1]));
            }
            MarkerReference marker =
                    raw.marker() == null
                            ? null
                            : new MarkerReference(
                                    raw.marker().owner(),
                                    names.get(raw.marker().protocol()),
                                    raw.marker().method(),
                                    raw.marker().providerPackage());
            types.add(
                    new TypePlan(
                            location(raw.shape()),
                            names.get(key),
                            raw.kind(),
                            List.copyOf(fields),
                            raw.expression() == null
                                    ? null
                                    : resolve(raw.expression(), builder.definitionExpressions, new ArrayList<>()),
                            List.copyOf(members),
                            marker,
                            raw.marker() != null && raw.kind() == Kind.OBJECT,
                            builder.declarationFieldSchemas.getOrDefault(key, ""),
                            builder.typeSources.get(key)));
        }
        declarations.sort(Comparator.comparing((String[] item) -> item[0]));
        List<DeclarationPlan> resultDeclarations = new ArrayList<>();
        for (String[] item : declarations) {
            String declarationKind = "";
            for (JsonNode owned : Json.list(vocabulary.path("ownedDeclarations"))) {
                if (owned.path("coordinate").asText().equals(item[0])) {
                    declarationKind = owned.path("kind").asText();
                    break;
                }
            }
            resultDeclarations.add(
                    new DeclarationPlan(
                            item[0], declarationKind, names.get(item[1]), names.get(item[2]), markerMethod(root, item[0])));
        }
        List<ImportedRaw> sortedImported = new ArrayList<>(imported);
        sortedImported.sort(Comparator.comparing(ImportedRaw::coordinate));
        List<ImportedDeclarationPlan> resultImported = new ArrayList<>();
        for (ImportedRaw item : sortedImported) {
            resultImported.add(
                    new ImportedDeclarationPlan(
                            item.coordinate(),
                            item.owner(),
                            item.kind(),
                            names.get(item.functionKey()),
                            names.get(item.protocolKey()),
                            item.method(),
                            item.providerPackage()));
        }
        return new EmissionPlan(
                target,
                names,
                List.copyOf(resultDeclarations),
                List.copyOf(resultImported),
                List.copyOf(types),
                builder.anyUsed,
                model.path("metadata").path("semanticSha256").asText());
    }
}
