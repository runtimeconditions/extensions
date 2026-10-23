package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.DiagnosticError.fail;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.networknt.schema.JsonSchema;
import com.networknt.schema.ValidationMessage;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.regex.Pattern;

public final class Target {
    public static final String API = "runtimeconditions.io/java-package-target/v1alpha1";
    public static final String KIND = "RuntimeConditionsJavaPackageTarget";
    private static final String SCHEMA_RESOURCE = "/target/package-target.schema.yaml";
    private static final Pattern DEPENDENCY = Pattern.compile("^/dependencies/\\d+(?:/\\w+)?$");
    private static final Pattern JAVA_PACKAGE =
            Pattern.compile("^[a-z][a-z0-9_]*(?:\\.[a-z][a-z0-9_]*)*$");

    public record Dependency(
            String extension,
            String groupId,
            String artifactId,
            String javaPackage,
            String version,
            String versionRange) {
        public Dependency(String extension, String groupId, String artifactId, String javaPackage, String version) {
            this(extension, groupId, artifactId, javaPackage, version, null);
        }

        public String coordinate() {
            return groupId + ":" + artifactId;
        }
    }

    public record PackageTarget(
            String packageKey,
            String rootExtension,
            String groupId,
            String artifactId,
            String javaPackage,
            String version,
            String sourceDirectory,
            String minimumJavaVersion,
            String publicationMode,
            String repositoryUrl,
            String registryId,
            List<Dependency> dependencies,
            String emitterSha256) {
        public String coordinate() {
            return groupId + ":" + artifactId;
        }

        public PackageTarget withEmitterSha256(String digest) {
            return new PackageTarget(
                    packageKey,
                    rootExtension,
                    groupId,
                    artifactId,
                    javaPackage,
                    version,
                    sourceDirectory,
                    minimumJavaVersion,
                    publicationMode,
                    repositoryUrl,
                    registryId,
                    dependencies,
                    digest);
        }
    }

    private static JsonSchema validator;

    private Target() {}

    private record Described(String code, String message, String pointer) {}

    private static String schemaText() {
        String override = System.getenv("RC_PACKAGE_TARGET_SCHEMA");
        try {
            if (override != null && !override.isEmpty()) {
                return Files.readString(Path.of(override), StandardCharsets.UTF_8);
            }
            try (InputStream stream = Target.class.getResourceAsStream(SCHEMA_RESOURCE)) {
                if (stream == null) {
                    throw fail("package-config", "RCJ1006", "package-target", "package target schema is unavailable");
                }
                return new String(stream.readAllBytes(), StandardCharsets.UTF_8);
            }
        } catch (IOException error) {
            throw fail("package-config", "RCJ1006", "package-target", "package target schema is unavailable");
        }
    }

    private static synchronized JsonSchema validator() {
        if (validator == null) {
            validator = Model.createValidator(schemaText());
        }
        return validator;
    }

    private static Described configError(String message, String pointer) {
        return new Described("RCJ1006", message, pointer);
    }

    private static Described describe(ValidationMessage error) {
        String path = error.getInstanceLocation().toString();
        String type = error.getType();
        Described registry = configError("registryId is required exactly for registry publication", "/registryId");
        if (type.equals("required")) {
            String missing = error.getProperty();
            if (path.isEmpty()) {
                return missing.equals("registryId") ? registry : configError("missing required field '" + missing + "'", "/" + missing);
            }
            return configError("missing dependency field '" + missing + "'", path + "/" + missing);
        }
        if (type.equals("additionalProperties")) {
            String extra = error.getProperty();
            return path.isEmpty()
                    ? configError("unknown field '" + extra + "'", "/" + extra)
                    : configError("unknown dependency field '" + extra + "'", path + "/" + extra);
        }
        if (type.equals("if") || type.equals("not")) {
            return registry;
        }
        return switch (path) {
            case "/apiVersion", "/kind" -> configError("unsupported target apiVersion or kind", "");
            case "/sourceDirectory" -> new Described("RCJ1007", "source directory does not match package key", path);
            case "/minimumJavaVersion" -> new Described("RCJ1008", "unsupported minimum Java version", path);
            case "/emitterSha256" -> configError("emitterSha256 must be a 64-character lowercase SHA-256 of the emitter tool", path);
            case "/version" -> configError("unsupported package version", path);
            case "/publicationMode" -> configError("unsupported publication mode", path);
            case "/repositoryUrl" -> configError("repositoryUrl must use https", path);
            case "/dependencies" -> configError("dependencies must be a sequence", path);
            default -> {
                if (DEPENDENCY.matcher(path).matches()) {
                    yield configError(
                            path.chars().filter(character -> character == '/').count() == 2
                                    ? "dependency must be a mapping"
                                    : "invalid dependency package metadata",
                            path);
                }
                yield configError("invalid " + path.substring(path.lastIndexOf('/') + 1), path);
            }
        };
    }

    private static int rank(ValidationMessage error) {
        if (!error.getInstanceLocation().toString().isEmpty()) {
            return 3;
        }
        return switch (error.getType()) {
            case "required" -> error.getProperty().equals("registryId") ? 2 : 0;
            case "additionalProperties" -> 1;
            case "if", "not" -> 2;
            default -> 3;
        };
    }

    private static String sortKey(ValidationMessage error) {
        return rank(error) + (error.getProperty() == null ? "" : error.getProperty()) + error.getInstanceLocation() + error.getType();
    }

    private static boolean validJavaPackage(String value) {
        if (!JAVA_PACKAGE.matcher(value).matches()) {
            return false;
        }
        for (String part : value.split("\\.")) {
            if (Naming.isReservedWord(part)) {
                return false;
            }
        }
        return true;
    }

    public static PackageTarget load(Path path) {
        ObjectNode value = YamlIo.load(path, false);
        List<ValidationMessage> errors = new ArrayList<>(Model.validate(validator(), value));
        String coordinate = value.path("packageKey").isTextual() ? value.path("packageKey").asText() : "package-target";
        if (!errors.isEmpty()) {
            errors.sort(Comparator.comparing(Target::sortKey));
            Described described = describe(errors.get(0));
            throw fail("package-config", described.code(), coordinate, described.message(), described.pointer());
        }
        String packageKey = value.get("packageKey").asText();
        if (!validJavaPackage(value.get("javaPackage").asText())) {
            throw fail("package-config", "RCJ1006", packageKey, "invalid javaPackage", "/javaPackage");
        }
        if (!value.get("sourceDirectory").asText().equals("bindings/" + packageKey + "/java")) {
            throw fail("package-config", "RCJ1007", packageKey, "source directory does not match package key", "/sourceDirectory");
        }
        List<Dependency> dependencies = new ArrayList<>();
        Set<String> seen = new HashSet<>();
        int index = 0;
        for (JsonNode entry : value.has("dependencies") ? value.get("dependencies") : Json.MAPPER.createArrayNode()) {
            String pointer = "/dependencies/" + index;
            index += 1;
            String extension = entry.get("extension").asText();
            if (!seen.add(extension)) {
                throw fail("package-config", "RCJ1006", packageKey, "duplicate dependency '" + extension + "'", pointer);
            }
            if (!validJavaPackage(entry.get("javaPackage").asText())) {
                throw fail("package-config", "RCJ1006", packageKey, "invalid dependency package metadata", pointer);
            }
            dependencies.add(
                    new Dependency(
                            extension,
                            entry.get("groupId").asText(),
                            entry.get("artifactId").asText(),
                            entry.get("javaPackage").asText(),
                            entry.get("version").asText(),
                            entry.has("versionRange") ? entry.get("versionRange").asText() : null));
        }
        return new PackageTarget(
                packageKey,
                value.get("rootExtension").asText(),
                value.get("groupId").asText(),
                value.get("artifactId").asText(),
                value.get("javaPackage").asText(),
                value.get("version").asText(),
                value.get("sourceDirectory").asText(),
                value.get("minimumJavaVersion").asText(),
                value.get("publicationMode").asText(),
                value.get("repositoryUrl").asText(),
                value.has("registryId") ? value.get("registryId").asText() : null,
                List.copyOf(dependencies),
                value.has("emitterSha256") ? value.get("emitterSha256").asText() : "");
    }
}
