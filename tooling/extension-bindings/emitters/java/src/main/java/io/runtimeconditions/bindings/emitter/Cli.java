package io.runtimeconditions.bindings.emitter;

import com.fasterxml.jackson.databind.node.ObjectNode;
import io.runtimeconditions.bindings.emitter.Plan.EmissionPlan;
import io.runtimeconditions.bindings.emitter.Target.PackageTarget;
import java.io.PrintStream;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

public final class Cli {
    private static final String USAGE =
            "usage: runtimeconditions-binding-emitter-java emit|verify|api --model FILE --package-config FILE (--output DIR | --input DIR)";
    private static final Set<String> OPTIONS = Set.of("--model", "--package-config", "--output", "--input");

    private Cli() {}

    public static int run(List<String> argv, PrintStream out, PrintStream err) {
        if (argv.isEmpty()) {
            err.println(USAGE);
            return 1;
        }
        String command = argv.get(0);
        if (command.equals("--version")) {
            out.println(Metadata.EMITTER_VERSION);
            return 0;
        }
        try {
            if (!command.equals("emit") && !command.equals("verify") && !command.equals("api")) {
                throw new IllegalArgumentException(USAGE);
            }
            Map<String, String> values = new LinkedHashMap<>();
            for (int index = 1; index < argv.size(); index += 2) {
                String name = argv.get(index);
                if (!OPTIONS.contains(name) || index + 1 >= argv.size() || values.containsKey(name)) {
                    throw new IllegalArgumentException(USAGE);
                }
                values.put(name, argv.get(index + 1));
            }
            ObjectNode result = Json.object();
            if (command.equals("api")) {
                if (!values.containsKey("--input")) {
                    throw new IllegalArgumentException(USAGE);
                }
                result.set("api", Json.MAPPER.valueToTree(Verification.apiSurface(Path.of(values.get("--input")))));
            } else {
                if (!values.containsKey("--model") || !values.containsKey("--package-config")) {
                    throw new IllegalArgumentException(USAGE);
                }
                ObjectNode model = Model.load(Path.of(values.get("--model")));
                PackageTarget target = Target.load(Path.of(values.get("--package-config")));
                EmissionPlan plan = Plan.build(model, target);
                if (command.equals("emit")) {
                    if (!values.containsKey("--output")) {
                        throw new IllegalArgumentException(USAGE);
                    }
                    Source.emitPackage(plan, model, Path.of(values.get("--output")));
                    result.put("output", values.get("--output"));
                } else {
                    if (!values.containsKey("--input")) {
                        throw new IllegalArgumentException(USAGE);
                    }
                    Verification.Verified verified = Verification.verifyPackage(plan, Path.of(values.get("--input")), model);
                    result.set("api", Json.MAPPER.valueToTree(verified.api()));
                    result.put("modelMapping", verified.modelMapping());
                }
            }
            out.println(Json.MAPPER.writeValueAsString(result));
            return 0;
        } catch (Exception error) {
            err.println(error.getMessage());
            return 1;
        }
    }
}
