package io.runtimeconditions.bindings.emitter;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import io.runtimeconditions.bindings.emitter.Naming.Style;
import io.runtimeconditions.bindings.emitter.Naming.Symbol;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import org.junit.jupiter.api.Test;

class NamingTest {
    @Test
    void namesAreDerivedFromTokens() {
        Object[][] cases = {
            {"base_url", Style.PASCAL, "BaseUrl"},
            {"base_url", Style.CAMEL, "baseUrl"},
            {"HTTPServer", Style.PASCAL, "HttpServer"},
            {"requestBody Schema", Style.CAMEL, "requestBodySchema"},
            {"key-value", Style.PASCAL, "KeyValue"},
            {"GET", Style.PASCAL, "Get"},
            {"3d", Style.PASCAL, "X3d"},
            {"3d", Style.CAMEL, "x3d"},
            {"streaming mode", Style.CONSTANT, "STREAMING_MODE"},
            {"Ünï cøde", Style.PASCAL, "ÜnïCøde"},
        };
        for (Object[] item : cases) {
            assertEquals(item[2], Naming.javaName((String) item[0], (Style) item[1], "coordinate"));
        }
    }

    @Test
    void reservedWordsGainATrailingUnderscore() {
        for (String word : List.of("class", "new", "record", "hashCode", "wait", "default")) {
            assertEquals(word + "_", Naming.javaName(word, Style.CAMEL, "c"));
        }
    }

    @Test
    void aSourceWithNoLettersOrDigitsHasNoIdentifier() {
        assertEquals("RCJ2000", assertThrows(DiagnosticError.class, () -> Naming.javaName("💥", Style.PASCAL, "c")).diagnostic().code());
        assertThrows(DiagnosticError.class, () -> Naming.javaName("---", Style.CAMEL, "c"));
    }

    @Test
    void exactNamesMustAlreadyBeIdentifiers() {
        assertEquals("rcMarker_0123", Naming.javaName("rcMarker_0123", Style.EXACT, "c"));
        assertThrows(DiagnosticError.class, () -> Naming.javaName("not-valid", Style.EXACT, "c"));
    }

    @Test
    void collisionsAreResolvedWithParentPathComponentsNearestFirst() {
        Map<String, String> names =
                Naming.allocateSymbols(
                        "pkg",
                        List.of(
                                Symbol.scoped("a", "c1", "mode", Style.PASCAL, List.of("http", "service")),
                                Symbol.scoped("b", "c2", "mode", Style.PASCAL, List.of("grpc", "service"))));
        assertEquals("HttpMode", names.get("a"));
        assertEquals("GrpcMode", names.get("b"));
    }

    @Test
    void aCollisionThatSurvivesTheFullPathStopsEmissionWithoutRenaming() {
        DiagnosticError error =
                assertThrows(
                        DiagnosticError.class,
                        () ->
                                Naming.allocateSymbols(
                                        "pkg",
                                        List.of(
                                                Symbol.scoped("a", "c1", "mode", Style.PASCAL, List.of("x")),
                                                Symbol.scoped("b", "c2", "Mode", Style.PASCAL, List.of("x")))));
        assertTrue(error.diagnostic().message().contains("after full path"));
    }

    @Test
    void fixedSymbolsAndJavaGlobalsAreNeverRenamed() {
        assertTrue(
                assertThrows(
                                DiagnosticError.class,
                                () ->
                                        Naming.allocateSymbols(
                                                "pkg",
                                                List.of(
                                                        Symbol.fixed("a", "c1", "service", Style.CAMEL),
                                                        Symbol.fixed("b", "c2", "Service", Style.CAMEL))))
                        .diagnostic()
                        .message()
                        .contains("fixed symbol"));
        assertTrue(
                assertThrows(
                                DiagnosticError.class,
                                () -> Naming.allocateSymbols("pkg", List.of(new Symbol("a", "c1", "String", Style.PASCAL))))
                        .diagnostic()
                        .message()
                        .contains("java-global:String"));
        assertTrue(
                assertThrows(
                                DiagnosticError.class,
                                () ->
                                        Naming.allocateSymbols(
                                                "pkg",
                                                List.of(
                                                        new Symbol("a", "c1", "x", Style.PASCAL),
                                                        new Symbol("a", "c2", "y", Style.PASCAL))))
                        .diagnostic()
                        .message()
                        .contains("duplicate symbol request key"));
    }

    @Test
    void allocationIsIndependentOfInputOrder() {
        List<Symbol> symbols =
                List.of(
                        Symbol.scoped("a", "c1", "mode", Style.PASCAL, List.of("http")),
                        Symbol.scoped("b", "c2", "mode", Style.PASCAL, List.of("grpc")),
                        new Symbol("c", "c3", "other", Style.CAMEL));
        List<Symbol> reversed = new ArrayList<>(symbols);
        java.util.Collections.reverse(reversed);
        assertEquals(new TreeMap<>(Naming.allocateSymbols("pkg", symbols)), new TreeMap<>(Naming.allocateSymbols("pkg", reversed)));
    }
}
