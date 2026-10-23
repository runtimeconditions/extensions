package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.TestSupport.TARGET;
import static io.runtimeconditions.bindings.emitter.TestSupport.delete;
import static io.runtimeconditions.bindings.emitter.TestSupport.emitterDigest;
import static io.runtimeconditions.bindings.emitter.TestSupport.modelPath;
import static io.runtimeconditions.bindings.emitter.TestSupport.temporaryDirectory;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;

class CliTest {
    record Result(int status, String stdout, String stderr) {}

    private static Result run(String... args) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        ByteArrayOutputStream err = new ByteArrayOutputStream();
        int status =
                Cli.run(
                        List.of(args),
                        new PrintStream(out, true, StandardCharsets.UTF_8),
                        new PrintStream(err, true, StandardCharsets.UTF_8));
        return new Result(status, out.toString(StandardCharsets.UTF_8), err.toString(StandardCharsets.UTF_8));
    }

    private static Path targetFile(Path directory) throws IOException {
        Path path = directory.resolve("target.yaml");
        Files.writeString(
                path,
                Files.readString(TARGET, StandardCharsets.UTF_8).replace("repositoryUrl:", "emitterSha256: " + emitterDigest() + "\nrepositoryUrl:"),
                StandardCharsets.UTF_8);
        return path;
    }

    @Test
    void emitVerifyAndApiWorkEndToEndThroughTheCommandLine() throws Exception {
        Path directory = temporaryDirectory();
        try {
            Path target = targetFile(directory);
            String model = modelPath("01-owned-kind-interface").toString();
            Path output = directory.resolve("package");
            Result emitted = run("emit", "--model", model, "--package-config", target.toString(), "--output", output.toString());
            assertEquals(0, emitted.status(), emitted.stderr());
            assertEquals(output.toString(), Json.MAPPER.readTree(emitted.stdout()).path("output").asText());
            Result verified = run("verify", "--model", model, "--package-config", target.toString(), "--input", output.toString());
            assertEquals(0, verified.status(), verified.stderr());
            JsonNode result = Json.MAPPER.readTree(verified.stdout());
            assertTrue(result.path("modelMapping").asBoolean());
            List<String> api = new ArrayList<>();
            result.path("api").forEach(item -> api.add(item.asText()));
            assertTrue(api.contains("method|Bindings|service|ServiceField... fields|Declaration|static"), api.toString());
            Result printed = run("api", "--input", output.toString());
            assertEquals(0, printed.status(), printed.stderr());
            assertEquals(result.path("api"), Json.MAPPER.readTree(printed.stdout()).path("api"));
        } finally {
            delete(directory);
        }
    }

    @Test
    void emitRefusesAnOccupiedOutputDirectoryAndWritesNothing() throws Exception {
        Path directory = temporaryDirectory();
        try {
            Path target = targetFile(directory);
            Path output = directory.resolve("occupied");
            Files.createDirectory(output);
            Files.writeString(output.resolve("keep.txt"), "keep");
            Result result =
                    run("emit", "--model", modelPath("01-owned-kind-interface").toString(), "--package-config", target.toString(), "--output", output.toString());
            assertEquals(1, result.status());
            assertTrue(result.stderr().contains("RCJ3001"), result.stderr());
            assertEquals("keep", Files.readString(output.resolve("keep.txt")));
        } finally {
            delete(directory);
        }
    }

    @Test
    void emitRequiresTheEmitterDigestForPackageResources() {
        Path directory = temporaryDirectory();
        try {
            Result result =
                    run("emit", "--model", modelPath("01-owned-kind-interface").toString(), "--package-config", TARGET.toString(), "--output", directory.resolve("out").toString());
            assertEquals(1, result.status());
            assertTrue(result.stderr().contains("emitterSha256"), result.stderr());
        } finally {
            delete(directory);
        }
    }

    @Test
    void badUsageAndMismatchedTargetsFailWithANonzeroStatus() throws Exception {
        assertEquals(1, run().status());
        assertEquals(1, run("generate").status());
        assertEquals(1, run("emit", "--model", "x").status());
        assertEquals(1, run("emit", "--bogus").status());
        assertEquals(1, run("api").status());
        assertEquals("0.1.0", run("--version").stdout().trim());
        Path directory = temporaryDirectory();
        try {
            Path target = targetFile(directory);
            Result result =
                    run("emit", "--model", modelPath("02-additive-field").toString(), "--package-config", target.toString(), "--output", directory.resolve("out").toString());
            assertEquals(1, result.status());
            assertTrue(result.stderr().contains("RCJ1009"), result.stderr());
        } finally {
            delete(directory);
        }
    }

    @Test
    void verifyRejectsATamperedApiOrManifest() throws Exception {
        Path directory = temporaryDirectory();
        try {
            Path target = targetFile(directory);
            String model = modelPath("01-owned-kind-interface").toString();
            Path output = directory.resolve("package");
            assertEquals(0, run("emit", "--model", model, "--package-config", target.toString(), "--output", output.toString()).status());
            Path bindings = output.resolve("src/main/java/io/runtimeconditions/bindings/conformance/ownedkindinterface/Http.java");
            String original = Files.readString(bindings);
            Files.writeString(bindings, original.replace("String endpoint", "Long endpoint"));
            Result tamperedApi = run("verify", "--model", model, "--package-config", target.toString(), "--input", output.toString());
            assertEquals(1, tamperedApi.status());
            assertTrue(tamperedApi.stderr().contains("native API or conformance mismatch"), tamperedApi.stderr());
            Files.writeString(bindings, original);
            Path manifest = output.resolve("src/main/resources/runtimeconditions/runtimeconditions.bindings.yaml");
            Files.writeString(manifest, Files.readString(manifest).replace("#schema", "#wrong"));
            Result tamperedManifest = run("verify", "--model", model, "--package-config", target.toString(), "--input", output.toString());
            assertEquals(1, tamperedManifest.status());
            assertTrue(tamperedManifest.stderr().contains("manifest mappings"), tamperedManifest.stderr());
        } finally {
            delete(directory);
        }
    }

    @Test
    void anOversizedInputIsRejectedWithoutReadingItAllIntoMemory() throws Exception {
        Path directory = temporaryDirectory();
        try {
            Path target = targetFile(directory);
            Path oversized = directory.resolve("model.yaml");
            try (java.io.RandomAccessFile file = new java.io.RandomAccessFile(oversized.toFile(), "rw")) {
                file.setLength(1L << 30);
            }
            Process process =
                    new ProcessBuilder(
                                    Path.of(System.getProperty("java.home"), "bin", "java").toString(),
                                    "-Xmx256m",
                                    "-cp",
                                    System.getProperty("java.class.path"),
                                    "io.runtimeconditions.bindings.emitter.Main",
                                    "emit",
                                    "--model",
                                    oversized.toString(),
                                    "--package-config",
                                    target.toString(),
                                    "--output",
                                    directory.resolve("package").toString())
                            .redirectErrorStream(true)
                            .start();
            String output = new String(process.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
            assertEquals(1, process.waitFor(), output);
            assertTrue(output.contains("RCJ1001") && output.contains("exceeds 64 MiB"), output);
            assertTrue(!output.contains("OutOfMemoryError"), output);
        } finally {
            delete(directory);
        }
    }
}
