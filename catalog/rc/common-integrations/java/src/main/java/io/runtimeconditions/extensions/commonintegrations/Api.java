package io.runtimeconditions.extensions.commonintegrations;

public final class Api {
    private static final Option OPTION = new OptionValue();

    private Api() {
    }

    public static final class Declaration {
        private Declaration() {
        }
    }

    public interface Option {
    }

    private static final class OptionValue implements Option {
    }

    public static Declaration declare(String name, Option... options) {
        return new Declaration();
    }

    public static Option spec(String format, String uri, String version) {
        return OPTION;
    }
}
