package io.runtimeconditions.bindings.emitter;

import com.fasterxml.jackson.databind.JsonNode;
import com.sun.source.tree.ClassTree;
import com.sun.source.tree.CompilationUnitTree;
import com.sun.source.tree.ExpressionStatementTree;
import com.sun.source.tree.ImportTree;
import com.sun.source.tree.LiteralTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.NewClassTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.VariableTree;
import com.sun.source.util.JavacTask;
import io.runtimeconditions.bindings.emitter.Plan.DeclarationPlan;
import io.runtimeconditions.bindings.emitter.Plan.EmissionPlan;
import io.runtimeconditions.bindings.emitter.Plan.FieldPlan;
import io.runtimeconditions.bindings.emitter.Plan.ImportedDeclarationPlan;
import io.runtimeconditions.bindings.emitter.Plan.Kind;
import io.runtimeconditions.bindings.emitter.Plan.TypePlan;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeSet;
import java.util.stream.Stream;
import javax.lang.model.element.Modifier;
import javax.tools.Diagnostic;
import javax.tools.DiagnosticCollector;
import javax.tools.JavaCompiler;
import javax.tools.JavaFileObject;
import javax.tools.StandardJavaFileManager;
import javax.tools.ToolProvider;

public final class Verification {
    private static final String MISMATCH = "native API or conformance mismatch";

    private Verification() {}

    private static void check(boolean condition, String description) {
        if (!condition) {
            throw new IllegalStateException(MISMATCH + ": " + description);
        }
    }

    private static String squash(Object tree) {
        return String.valueOf(tree).replaceAll("\\s+", " ").trim();
    }

    record ParsedType(String path, String packageName, List<String> imports, ClassTree type) {}

    static Map<String, ParsedType> parse(Path directory) {
        Path root = directory.resolve("src/main/java");
        List<Path> files = List.of();
        // A package that declares and imports nothing of its own has no Java source directory.
        if (Files.isDirectory(root)) {
            try (Stream<Path> walk = Files.walk(root)) {
                files = walk.filter(path -> path.toString().endsWith(".java")).sorted().toList();
            } catch (IOException error) {
                throw new IllegalStateException(MISMATCH + ": cannot read " + root);
            }
        }
        Map<String, ParsedType> result = new LinkedHashMap<>();
        if (files.isEmpty()) {
            return result;
        }
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        DiagnosticCollector<JavaFileObject> diagnostics = new DiagnosticCollector<>();
        try (StandardJavaFileManager manager =
                compiler.getStandardFileManager(diagnostics, null, StandardCharsets.UTF_8)) {
            JavacTask task =
                    (JavacTask)
                            compiler.getTask(
                                    null,
                                    manager,
                                    diagnostics,
                                    List.of("-proc:none"),
                                    null,
                                    manager.getJavaFileObjectsFromPaths(files));
            for (CompilationUnitTree unit : task.parse()) {
                String path = directory.relativize(Path.of(unit.getSourceFile().toUri())).toString();
                List<? extends Tree> declarations = unit.getTypeDecls();
                check(declarations.size() == 1, path + " declares exactly one top-level type");
                ClassTree type = (ClassTree) declarations.get(0);
                List<String> imports = new ArrayList<>();
                for (ImportTree item : unit.getImports()) {
                    imports.add(squash(item.getQualifiedIdentifier()));
                }
                String name = type.getSimpleName().toString();
                check(path.endsWith("/" + name + ".java"), path + " is named after its type");
                result.put(name, new ParsedType(path, squash(unit.getPackageName()), imports, type));
            }
        } catch (IOException error) {
            throw new IllegalStateException(MISMATCH + ": cannot parse generated sources");
        }
        for (Diagnostic<? extends JavaFileObject> item : diagnostics.getDiagnostics()) {
            check(item.getKind() != Diagnostic.Kind.ERROR, "generated source parses: " + item.getMessage(null));
        }
        return result;
    }

    private static boolean nullable(String annotation) {
        return annotation.equals("Void") || annotation.equals("Object");
    }

    private record Component(String name, String type) {}

    private static List<Component> components(ClassTree type) {
        List<Component> result = new ArrayList<>();
        for (Tree member : type.getMembers()) {
            if (member instanceof VariableTree variable
                    && variable.getModifiers().getFlags().contains(Modifier.PRIVATE)
                    && variable.getModifiers().getFlags().contains(Modifier.FINAL)
                    && !variable.getModifiers().getFlags().contains(Modifier.STATIC)) {
                result.add(new Component(variable.getName().toString(), squash(variable.getType())));
            }
        }
        return result;
    }

    private static Set<String> requiredChecks(ClassTree type) {
        Set<String> result = new TreeSet<>();
        for (Tree member : type.getMembers()) {
            if (member instanceof MethodTree method && method.getName().contentEquals("<init>") && method.getBody() != null) {
                for (StatementTree statement : method.getBody().getStatements()) {
                    if (statement instanceof ExpressionStatementTree expression
                            && expression.getExpression() instanceof MethodInvocationTree call
                            && squash(call.getMethodSelect()).endsWith("Objects.requireNonNull")
                            && !call.getArguments().isEmpty()) {
                        result.add(squash(call.getArguments().get(0)));
                    }
                }
            }
        }
        return result;
    }

    private static List<MethodTree> methods(ClassTree type) {
        List<MethodTree> result = new ArrayList<>();
        for (Tree member : type.getMembers()) {
            if (member instanceof MethodTree method && !method.getName().contentEquals("<init>")) {
                result.add(method);
            }
        }
        return result;
    }

    private static List<String> implemented(ClassTree type) {
        List<String> result = new ArrayList<>();
        for (Tree item : type.getImplementsClause()) {
            result.add(squash(item));
        }
        return result;
    }

    private static List<String> expectedFiles(EmissionPlan plan) {
        List<String> result = new ArrayList<>();
        for (String name : Source.exportedNames(plan)) {
            result.add(Source.sourcePath(plan, name));
        }
        Collections.sort(result);
        return result;
    }

    private static void checkRecord(ClassTree type, List<FieldPlan> fields, String owner, String marker, List<String> markerProtocol) {
        List<Component> actual = components(type);
        List<Component> expected = new ArrayList<>();
        for (FieldPlan field : fields) {
            expected.add(new Component(field.name(), field.annotation()));
        }
        check(actual.equals(expected), owner + " components match the plan");
        Set<String> checked = requiredChecks(type);
        for (FieldPlan field : fields) {
            if (!nullable(field.annotation())) {
                check(checked.contains(field.name()) == field.required(), owner + "." + field.name() + " requiredness");
            }
        }
        List<String> names = new ArrayList<>();
        for (MethodTree method : methods(type)) {
            names.add(method.getName().toString());
        }
        check(names.equals(marker == null ? List.of() : List.of(marker)), owner + " markers match the plan");
        check(implemented(type).equals(markerProtocol), owner + " implements its marker");
    }

    private static void checkPublicApi(EmissionPlan plan, Path directory) {
        Map<String, ParsedType> parsed = parse(directory);
        List<String> actualFiles = new ArrayList<>();
        for (ParsedType item : parsed.values()) {
            actualFiles.add(item.path().replace('\\', '/'));
        }
        Collections.sort(actualFiles);
        check(actualFiles.equals(expectedFiles(plan)), "source files == planned types, protocols, and declaration classes");
        for (ParsedType item : parsed.values()) {
            check(item.packageName().equals(plan.target().javaPackage()), item.path() + " package");
            check(item.type().getModifiers().getFlags().contains(Modifier.PUBLIC), item.path() + " is public");
        }
        if (!plan.declarations().isEmpty()) {
            ClassTree declaration = parsed.get(Plan.DECLARATION_CLASS).type();
            check(declaration.getKind() == Tree.Kind.CLASS, "Declaration is a class");
            ClassTree functions = parsed.get(Plan.FUNCTIONS_CLASS).type();
            check(functions.getKind() == Tree.Kind.CLASS, "Bindings is a class");
            check(functions.getModifiers().getFlags().contains(Modifier.FINAL), "Bindings is final");
            Map<String, MethodTree> byName = new LinkedHashMap<>();
            for (MethodTree method : methods(functions)) {
                byName.put(method.getName().toString(), method);
            }
            Set<String> expectedFunctions = new TreeSet<>();
            for (DeclarationPlan item : plan.declarations()) {
                expectedFunctions.add(item.function());
            }
            check(new TreeSet<>(byName.keySet()).equals(expectedFunctions), "functions == declaration functions");
            for (DeclarationPlan item : plan.declarations()) {
                MethodTree method = byName.get(item.function());
                check(method.getModifiers().getFlags().contains(Modifier.STATIC), "declaration function is static");
                check(method.getParameters().size() == 1, "declaration takes one varargs parameter");
                check(
                        squash(method.getParameters().get(0)).equals(item.protocol() + "... fields"),
                        "declaration takes rest fields named fields");
                check(squash(method.getReturnType()).equals(Plan.DECLARATION_CLASS), "declaration returns Declaration");
                ClassTree protocol = parsed.get(item.protocol()).type();
                check(protocol.getKind() == Tree.Kind.INTERFACE, item.protocol() + " is an interface");
                List<MethodTree> protocolMethods = methods(protocol);
                check(
                        protocolMethods.size() == 1
                                && protocolMethods.get(0).getName().contentEquals(item.method())
                                && protocolMethods.get(0).getParameters().isEmpty()
                                && squash(protocolMethods.get(0).getReturnType()).equals("void"),
                        "protocol has only the marker method");
            }
        }
        for (ImportedDeclarationPlan item : plan.importedDeclarations()) {
            check(!parsed.containsKey(item.protocol()), "imported declaration is not redeclared");
        }
        for (TypePlan item : plan.types()) {
            ClassTree type = parsed.get(item.name()).type();
            if (item.kind() == Kind.ENUM) {
                check(type.getKind() == Tree.Kind.ENUM, item.name() + " is an enum");
                List<List<String>> members = new ArrayList<>();
                for (Tree member : type.getMembers()) {
                    if (member instanceof VariableTree variable && variable.getInitializer() instanceof NewClassTree creation) {
                        members.add(List.of(variable.getName().toString(), String.valueOf(((LiteralTree) creation.getArguments().get(0)).getValue())));
                    }
                }
                List<List<String>> expected = new ArrayList<>();
                for (Plan.Member member : item.members()) {
                    expected.add(List.of(member.name(), member.value()));
                }
                check(members.equals(expected), "enum " + item.name() + " members match the plan");
            } else if (item.kind() == Kind.ALIAS) {
                if (item.expression().kind().equals("union")) {
                    check(type.getKind() == Tree.Kind.INTERFACE, item.name() + " is a union interface");
                    check(type.getModifiers().getFlags().contains(Modifier.SEALED), item.name() + " is sealed");
                    List<ClassTree> variants = new ArrayList<>();
                    for (Tree member : type.getMembers()) {
                        if (member instanceof ClassTree nested) {
                            variants.add(nested);
                        }
                    }
                    check(variants.size() == item.expression().arguments().size(), item.name() + " variant count");
                    for (int index = 0; index < variants.size(); index += 1) {
                        ClassTree variant = variants.get(index);
                        check(variant.getKind() == Tree.Kind.RECORD, item.name() + " variants are records");
                        check(variant.getSimpleName().contentEquals("Variant" + (index + 1)), item.name() + " variant names");
                        String annotation = Plan.annotate(item.expression().arguments().get(index), plan.names());
                        checkRecord(
                                variant,
                                List.of(new FieldPlan("", "value", "value", item.expression().arguments().get(index), annotation, true)),
                                item.name() + ".Variant" + (index + 1),
                                null,
                                List.of(item.name()));
                    }
                } else {
                    check(type.getKind() == Tree.Kind.RECORD, item.name() + " is a wrapper record");
                    String annotation = Plan.annotate(item.expression(), plan.names());
                    checkRecord(
                            type,
                            List.of(new FieldPlan("", "value", "value", item.expression(), annotation, true)),
                            item.name(),
                            null,
                            List.of());
                }
            } else {
                check(type.getKind() == Tree.Kind.RECORD, item.name() + " is a record");
                checkRecord(
                        type,
                        item.fields(),
                        item.name(),
                        item.marker() == null ? null : item.marker().method(),
                        item.marker() == null ? List.of() : List.of(item.marker().protocol()));
            }
        }
    }

    public static List<String> apiSurface(Path directory) {
        Map<String, ParsedType> parsed = parse(directory);
        List<String> result = new ArrayList<>();
        for (Map.Entry<String, ParsedType> entry : parsed.entrySet()) {
            String name = entry.getKey();
            ClassTree type = entry.getValue().type();
            for (String item : entry.getValue().imports()) {
                if (!item.startsWith("java.")) {
                    int dot = item.lastIndexOf('.');
                    result.add("import|" + item.substring(0, dot) + "|" + item.substring(dot + 1));
                }
            }
            describe(result, name, type);
        }
        Collections.sort(result);
        return result;
    }

    private static void describe(List<String> result, String name, ClassTree type) {
        String heritage = String.join(",", implemented(type));
        switch (type.getKind()) {
            case INTERFACE -> result.add("interface|" + name + (type.getModifiers().getFlags().contains(Modifier.SEALED) ? "|sealed" : ""));
            case ENUM -> result.add("enum|" + name);
            case RECORD -> result.add("record|" + name + "|" + heritage);
            default -> result.add("class|" + name + "|" + heritage);
        }
        if (type.getKind() == Tree.Kind.RECORD) {
            Set<String> checked = requiredChecks(type);
            for (Component component : components(type)) {
                String requiredness = nullable(component.type()) ? "nullable" : checked.contains(component.name()) ? "required" : "optional";
                result.add("component|" + name + "|" + component.name() + "|" + component.type() + "|" + requiredness);
            }
        }
        for (Tree member : type.getMembers()) {
            if (member instanceof VariableTree variable && variable.getInitializer() instanceof NewClassTree creation) {
                result.add("member|" + name + "|" + variable.getName() + "(" + squash(creation.getArguments().get(0)) + ")");
            } else if (member instanceof MethodTree method && !method.getName().contentEquals("<init>")) {
                String parameters = String.join(",", method.getParameters().stream().map(Verification::squash).toList());
                String modifiers = method.getModifiers().getFlags().contains(Modifier.STATIC) ? "static" : "";
                result.add(
                        "method|" + name + "|" + method.getName() + "|" + parameters + "|" + squash(method.getReturnType()) + "|" + modifiers);
            } else if (member instanceof ClassTree nested) {
                describe(result, name + "." + nested.getSimpleName(), nested);
            }
        }
    }

    public record Verified(List<String> api, boolean modelMapping) {}

    public static Verified verifyPackage(EmissionPlan plan, Path directory, JsonNode model) {
        checkPublicApi(plan, directory);
        Map<String, String> expected = Metadata.renderResources(plan, model);
        for (String path : List.of("pom.xml", Metadata.MANIFEST_PATH)) {
            String actual;
            try {
                actual = Files.readString(directory.resolve(path), StandardCharsets.UTF_8);
            } catch (IOException error) {
                throw new IllegalStateException(path + " is missing from the package");
            }
            if (!actual.equals(expected.get(path))) {
                throw new IllegalStateException(
                        path.equals("pom.xml")
                                ? "pom.xml differs from the package target and normalized model"
                                : "binding manifest mappings differ from normalized model");
            }
        }
        return new Verified(apiSurface(directory), true);
    }
}
