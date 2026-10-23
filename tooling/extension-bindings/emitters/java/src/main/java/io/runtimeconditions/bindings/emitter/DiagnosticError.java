package io.runtimeconditions.bindings.emitter;

import java.io.Serializable;

public final class DiagnosticError extends RuntimeException {
    private static final long serialVersionUID = 1L;

    public record Diagnostic(
            String category, String code, String coordinate, String message, String jsonPointer)
            implements Serializable {}

    private final Diagnostic diagnostic;

    public DiagnosticError(Diagnostic diagnostic) {
        super(
                diagnostic.code()
                        + " "
                        + (diagnostic.jsonPointer().isEmpty()
                                ? diagnostic.coordinate()
                                : diagnostic.coordinate() + " " + diagnostic.jsonPointer())
                        + ": "
                        + diagnostic.message());
        this.diagnostic = diagnostic;
    }

    public Diagnostic diagnostic() {
        return diagnostic;
    }

    public static DiagnosticError fail(
            String category, String code, String coordinate, String message) {
        return fail(category, code, coordinate, message, "");
    }

    public static DiagnosticError fail(
            String category, String code, String coordinate, String message, String jsonPointer) {
        return new DiagnosticError(new Diagnostic(category, code, coordinate, message, jsonPointer));
    }
}
