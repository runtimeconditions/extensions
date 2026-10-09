# Environment Configuration Extension Specification (Draft)

## Status

**Draft — Request for Comments**

This document defines a first-party Runtime Conditions extension for declaring workload configuration inputs that are provided through environment variables.

The machine-readable extension definition is [env-configuration-v1alpha1.yaml](env-configuration-v1alpha1.yaml).

This extension is not part of the core Runtime Conditions Profile vocabulary. Profiles that use this extension MUST declare it in the top-level `extensions` list.

---

# 1. Extension Identity

```yaml
extensions:
  - https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml
  - https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml
```

This extension defines the Condition field `configuration`.

This extension depends on `https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml` for the common Condition kinds and interface types referenced by its standard property validation table.

This extension owns the unqualified Condition field name `configuration` only
within the common integration scopes listed in this document. Other extensions
MAY define `configuration` for their own non-overlapping Condition kinds and
interface types.

---

# 2. Purpose

The Environment Configuration extension describes the environment variable names a workload expects to read in order to consume a fulfilled Condition.

This extension describes:

- Which environment variables are read by the workload
- Which connection property each environment variable represents
- Whether an environment variable carries sensitive material
- Whether an environment variable is required within a configuration set
- Alternative sets of environment variables supported by the workload

This extension does not describe:

- Concrete target-environment values
- Infrastructure resources
- Deployment topology
- Provider-specific provisioning behavior
- Default values used by application code
- Validation rules for the contents of environment variable values

Environment variable values are strings. This extension declares how strings are passed to the workload, not what those strings contain.

---

# 3. Configuration Field

A Condition MAY include a `configuration` field when this extension is declared.

The `configuration` field MUST contain exactly one of:

- `env`
- `alternatives`

Use `configuration.env` when the workload expects one configuration set:

```yaml
conditions:
  - name: todos-api
    kind: api
    interface:
      type: http
      operations:
        - method: GET
          path: /todos/{id}
    configuration:
      env:
        - property: baseUrl
          name: TODOS_API_URL
        - property: token
          name: TODOS_API_TOKEN
          sensitive: true
```

Use `configuration.alternatives` when the workload supports two or more complete configuration sets:

```yaml
conditions:
  - name: request-cache
    kind: cache
    interface:
      type: key_value
      engine: redis
    configuration:
      alternatives:
        - env:
            - property: url
              name: REDIS_URL
        - env:
            - property: hostname
              name: REDIS_HOST
            - property: port
              name: REDIS_PORT
              required: false
```

The `alternatives` field describes valid input sets, not precedence. If workload code supports multiple alternatives, the workload code determines how to handle precedence when more than one alternative is provided.

Platform automation MUST satisfy at least one complete configuration set for a required Condition. Platform automation MAY provide more than one complete configuration set.

---

# 4. Environment Variable Inputs

Each item in an `env` list describes one environment variable expected by the workload.

| Field       | Required | Description |
| ----------- | -------- | ----------- |
| `property`  | YES      | Standard connection property represented by the environment variable |
| `name`      | YES      | Environment variable name read by the workload |
| `sensitive` | NO       | Whether the value is sensitive and should be handled as secret material. Defaults to `false` |
| `required`  | NO       | Whether this input is required within its configuration set. Defaults to `true` |

Environment variable names MUST match the following pattern:

```text
^[A-Za-z_][A-Za-z0-9_]*$
```

An environment variable input with `required: false` declares that the workload may read the variable but does not require it to be present for the configuration set to be valid.

---

# 5. Standard Connection Properties

The following standard connection properties are defined by this extension:

| Property   | Description |
| ---------- | ----------- |
| `url`      | Complete connection URL for a dependency |
| `baseUrl`  | Base URL used when calling an API dependency |
| `hostname` | DNS name, host name, or network host for a dependency |
| `port`     | Network port |
| `scheme`   | Connection or request scheme |
| `username` | Username or client identifier |
| `password` | Password or shared secret |
| `database` | Database name, schema name, logical database number, or equivalent datastore selector |
| `token`    | Bearer token, API token, or equivalent credential |
| `tls`      | Whether TLS is required or enabled |

Allowed properties are validated against the Condition `kind` and `interface.type`.

| Condition | Allowed properties |
| --------- | ------------------ |
| `kind: api`, `interface.type: http` | `url`, `baseUrl`, `hostname`, `port`, `scheme`, `username`, `password`, `token`, `tls` |
| `kind: datastore`, `interface.type: relational` | `url`, `hostname`, `port`, `scheme`, `username`, `password`, `database`, `tls` |
| `kind: datastore`, `interface.type: document` | `url`, `hostname`, `port`, `scheme`, `username`, `password`, `database`, `tls` |
| `kind: cache`, `interface.type: key_value` | `url`, `hostname`, `port`, `scheme`, `username`, `password`, `database`, `token`, `tls` |

Extensions that define additional Condition kinds or interface types MAY define additional allowed properties for use with this extension.

---

# 6. Validation

A Condition that uses this extension is invalid if:

- `configuration` appears and `https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml` is not declared
- `configuration` contains neither `env` nor `alternatives`
- `configuration` contains both `env` and `alternatives`
- `configuration.env` is present but empty
- `configuration.alternatives` contains fewer than two alternatives
- Any configuration alternative does not declare exactly one `env` list
- Any configuration alternative declares an empty `env` list
- Any `env` item omits `property`
- Any `env` item omits `name`
- Any `env.name` does not match the required environment variable name pattern
- Any `env.property` is not allowed for the Condition `kind` and `interface.type`
- Any `env.sensitive` value is not a boolean
- Any `env.required` value is not a boolean
- Any configuration set declares the same `property` more than once
- Any configuration set declares the same environment variable `name` more than once

---

# 7. Example Profile

```yaml
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsProfile

metadata:
  name: request-logger

workload:
  uri: https://github.com/example-org/request-logger
  version: v1.2.3

extensions:
  - https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml
  - https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml

conditions:
  - name: todos-api
    kind: api
    interface:
      type: http
      operations:
        - method: GET
          path: /todos/{id}
    configuration:
      env:
        - property: baseUrl
          name: TODOS_API_URL

  - name: request-cache
    kind: cache
    interface:
      type: key_value
      engine: redis
    configuration:
      alternatives:
        - env:
            - property: url
              name: REDIS_URL
        - env:
            - property: hostname
              name: REDIS_HOST
            - property: port
              name: REDIS_PORT
              required: false
```

---

# 8. Summary

The Environment Configuration extension defines a portable workload input contract for environment-variable-based dependency configuration while preserving the core Runtime Conditions Profile boundary between runtime integration requirements and concrete fulfillment values.
