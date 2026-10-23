package io.runtimeconditions.bindings.emitter;

import java.util.List;

public final class Main {
    private Main() {}

    public static void main(String[] args) {
        System.exit(Cli.run(List.of(args), System.out, System.err));
    }
}
