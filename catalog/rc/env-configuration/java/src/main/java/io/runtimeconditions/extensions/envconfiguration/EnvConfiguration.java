package io.runtimeconditions.extensions.envconfiguration;

import io.runtimeconditions.extensions.commonintegrations.Api;
import io.runtimeconditions.extensions.commonintegrations.Cache;
import io.runtimeconditions.extensions.commonintegrations.Datastore;

public final class EnvConfiguration {
    private static final ConditionOption CONDITION_OPTION = new ConditionOptionValue();
    private static final EnvOption ENV_OPTION = new EnvOptionValue();

    private EnvConfiguration() {
    }

    public interface EnvOption {
    }

    public interface ConditionOption
            extends Api.Option,
                    Datastore.Option,
                    Cache.Option {
    }

    private static final class EnvOptionValue implements EnvOption {
    }

    private static final class ConditionOptionValue implements ConditionOption {
    }

    public static ConditionOption env(String property, String name, EnvOption... options) {
        return CONDITION_OPTION;
    }

    public static ConditionOption envAlternative(ConditionOption... inputs) {
        return CONDITION_OPTION;
    }

    public static EnvOption sensitive() {
        return ENV_OPTION;
    }

    public static EnvOption optional() {
        return ENV_OPTION;
    }
}
