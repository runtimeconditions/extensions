package io.runtimeconditions.extensions.commonintegrations;

public final class Http {
    private static final OperationValue OPERATION = new OperationValue();
    private static final SchemaValue SCHEMA = new SchemaValue();

    private Http() {
    }

    public interface OperationOption {
    }

    public interface SchemaOption extends Api.Option, OperationOption {
    }

    private static final class OperationValue implements Api.Option, OperationOption {
    }

    private static final class SchemaValue implements SchemaOption {
    }

    public static Api.Option get(String path, OperationOption... options) {
        return OPERATION;
    }

    public static Api.Option head(String path, OperationOption... options) {
        return OPERATION;
    }

    public static Api.Option post(String path, OperationOption... options) {
        return OPERATION;
    }

    public static Api.Option put(String path, OperationOption... options) {
        return OPERATION;
    }

    public static Api.Option patch(String path, OperationOption... options) {
        return OPERATION;
    }

    public static Api.Option delete(String path, OperationOption... options) {
        return OPERATION;
    }

    public static Api.Option options(String path, OperationOption... options) {
        return OPERATION;
    }

    public static Api.Option trace(String path, OperationOption... options) {
        return OPERATION;
    }

    public static <T> SchemaOption request(Class<T> schemaType) {
        return SCHEMA;
    }

    public static <T> SchemaOption response(Class<T> schemaType) {
        return SCHEMA;
    }
}
