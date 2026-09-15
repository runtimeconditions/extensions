package io.runtimeconditions.extensions.commonintegrations;

public final class Cache {
    private static final Option OPTION = new OptionValue();

    private Cache() {
    }

    public static final class Declaration {
        private Declaration() {
        }
    }

    public enum Engine {
        REDIS("redis"),
        MEMCACHED("memcached");

        private final String value;

        Engine(String value) {
            this.value = value;
        }

        public String value() {
            return value;
        }
    }

    public interface Option {
    }

    private static final class OptionValue implements Option {
    }

    public static Declaration declare(String name, Option... options) {
        return new Declaration();
    }

    public static Option keyValue(Engine engine) {
        return OPTION;
    }
}
