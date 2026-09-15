package io.runtimeconditions.extensions.commonintegrations;

public final class Datastore {
    private static final Option OPTION = new OptionValue();

    private Datastore() {
    }

    public static final class Declaration {
        private Declaration() {
        }
    }

    public enum Engine {
        POSTGRES("postgres"),
        MYSQL("mysql"),
        MARIADB("mariadb"),
        SQLSERVER("sqlserver"),
        ORACLE("oracle"),
        SQLITE("sqlite"),
        MONGODB("mongodb"),
        COUCHBASE("couchbase");

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

    public static Option relational(Engine engine) {
        return OPTION;
    }

    public static Option document(Engine engine) {
        return OPTION;
    }
}
