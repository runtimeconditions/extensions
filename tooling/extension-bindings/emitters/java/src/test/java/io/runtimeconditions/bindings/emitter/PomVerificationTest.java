package io.runtimeconditions.bindings.emitter;

import static io.runtimeconditions.bindings.emitter.TestSupport.buildCase;
import static io.runtimeconditions.bindings.emitter.TestSupport.delete;
import static io.runtimeconditions.bindings.emitter.TestSupport.temporaryDirectory;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import io.runtimeconditions.bindings.emitter.TestSupport.Built;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.function.UnaryOperator;
import org.junit.jupiter.api.Test;

/** verify compares the generated pom.xml with the one the target and model produce. */
final class PomVerificationTest {
    private static void expectRejected(String name, UnaryOperator<String> change, boolean delete) throws IOException {
        Path workspace = temporaryDirectory();
        try {
            Built built = buildCase("03-transitive-closure", workspace, null);
            Verification.verifyPackage(built.plan(), built.directory(), built.model());
            Path pom = built.directory().resolve("pom.xml");
            if (delete) {
                Files.delete(pom);
            } else {
                String text = Files.readString(pom);
                String changed = change.apply(text);
                if (changed.equals(text)) {
                    throw new IllegalStateException(name + " did not change the pom");
                }
                Files.writeString(pom, changed);
            }
            IllegalStateException error =
                    assertThrows(
                            IllegalStateException.class,
                            () -> Verification.verifyPackage(built.plan(), built.directory(), built.model()),
                            name);
            assertEquals(delete ? "pom.xml is missing from the package" : "pom.xml differs from the package target and normalized model", error.getMessage());
        } finally {
            delete(workspace);
        }
    }

    @Test
    void aMissingOrChangedPomFailsVerification() throws IOException {
        expectRejected("missing pom", text -> text, true);
        expectRejected("changed coordinates", text -> text.replace("<groupId>io.runtimeconditions.conformance</groupId>", "<groupId>other</groupId>"), false);
        expectRejected("wrong Java release", text -> text.replaceFirst("<maven.compiler.release>\\d+<", "<maven.compiler.release>99<"), false);
        expectRejected("dropped dependency", text -> text.replaceFirst("(?s)  <dependencies>.*?</dependencies>\n", ""), false);
    }
}
