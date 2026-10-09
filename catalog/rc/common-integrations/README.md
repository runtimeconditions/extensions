# Common Integrations Extension Specification (Draft)

## Status

**Draft — Request for Comments**

This document defines a first-party Runtime Conditions extension for common application runtime integrations.

The machine-readable extension definition is [common-integrations-v1alpha1.yaml](common-integrations-v1alpha1.yaml).

This extension is not part of the core Runtime Conditions Profile vocabulary. Profiles that use this extension MUST declare it in the top-level `extensions` list.

---

# 1. Extension Identity

```yaml
extensions:
  - https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml
```

This extension defines:

- Common Condition kinds
- Common interface types
- Common interface fields
- Common engine values
- Validation constraints for common integration declarations

This extension owns the unqualified vocabulary it defines within the scopes described in this document. Other extensions MAY depend on this extension and reference that vocabulary, but MUST NOT redefine the same unqualified vocabulary in the same ownership scope.

---

# 2. Condition Kinds

This extension defines the following Condition kinds:

| Kind | Description |
| ---- | ----------- |
| `api` | External service integrations such as APIs |
| `datastore` | Persistent data storage systems |
| `cache` | Volatile data storage optimized for fast access |

These kinds are intentionally broad so that multiple interaction models or implementation families can be expressed within the same integration class through the `interface` block.

---

# 3. API Interfaces

API interfaces describe callable external services.

## 3.1 HTTP Interface

The `api` kind supports the `http` interface type.

```yaml
kind: api
interface:
  type: http
  spec:
    format: openapi
    uri: https://github.com/example-org/example-service/openapi.yaml
    version: ^1.2.0
  operations:
    - method: POST
      path: /charge
      requestBodySchema:
        amount: integer
        currency: string
      responseSchema:
        id: string
        status: string
```

## 3.2 HTTP Interface Fields

| Field | Required | Description |
| ----- | -------- | ----------- |
| `type` | YES | Interface type. Must be `http` |
| `spec` | NO | Reference to an external API specification document |
| `operations` | NO | Explicit list of operations the workload depends on |

At least one of `spec` or `operations` MUST be present. Both MAY be declared together. When both are present and disagree, the `operations` declaration takes precedence over `spec`.

## 3.3 API Specification Fields

The `spec` block references an external API specification document.

```yaml
spec:
  format: openapi
  uri: https://github.com/example-org/example-service/openapi.yaml
  version: ^1.2.0
```

| Field | Required | Description |
| ----- | -------- | ----------- |
| `format` | YES | API specification format. Only `openapi` is currently supported |
| `uri` | YES | Location of the external specification document |
| `version` | NO | Required version of the referenced document, as an exact version or version constraint |

Only the OpenAPI specification is currently supported. Other API specification formats MAY be supported by future extensions.

## 3.4 Version Constraint Syntax

`interface.spec.version` declares which version of the referenced specification document the workload requires. It accepts either an exact version or a version constraint expression.

Versions MUST follow Semantic Versioning, expressed as `MAJOR.MINOR.PATCH`.

| Syntax | Name | Meaning |
| ------ | ---- | ------- |
| `1.2.3` | Exact | Matches only `1.2.3` |
| `=1.2.3` | Exact | Equivalent to `1.2.3` |
| `>1.2.3` | Greater than | Matches any version higher than `1.2.3` |
| `>=1.2.3` | Minimum | Matches `1.2.3` and any higher version |
| `<1.2.3` | Less than | Matches any version lower than `1.2.3` |
| `<=1.2.3` | Maximum | Matches `1.2.3` and any lower version |
| `^1.2.3` | Compatible | Matches `>=1.2.3` and `<2.0.0` |
| `~1.2.3` | Approximate | Matches `>=1.2.3` and `<1.3.0` |

When `version` is omitted, no version constraint is applied.

## 3.5 Operation Fields

| Field | Required | Description |
| ----- | -------- | ----------- |
| `method` | YES | HTTP method |
| `path` | YES | Request path |
| `requestBodySchema` | NO | Minimum required request body fields and their types |
| `responseSchema` | NO | Minimum required response fields and their types |

Allowed HTTP methods are:

- GET
- HEAD
- POST
- PUT
- PATCH
- DELETE
- OPTIONS
- TRACE

## 3.6 Schema Fields

`requestBodySchema` and `responseSchema` describe the data structures an operation depends on. Each is expressed as a map whose keys are field names and whose values declare the JSON Schema type of each field.

The declared fields represent the minimum set of fields that MUST be present in the external API. The external service MAY expose additional fields beyond those declared; only the declared fields participate in matching.

A type declaration is one of:

- A JSON Schema type keyword: `string`, `number`, `integer`, `boolean`, or `null`
- A nested object, declared by mapping field names to further type declarations
- An array, declared as a single-element list containing the element type declaration

---

# 4. Datastore Interfaces

Datastore interfaces describe persistent storage systems.

## 4.1 Relational Interface

The `datastore` kind supports the `relational` interface type.

```yaml
kind: datastore
interface:
  type: relational
  engine: postgres
```

Allowed `engine` values for `type: relational`:

- `postgres`
- `mysql`
- `mariadb`
- `sqlserver`
- `oracle`
- `sqlite`

## 4.2 Document Interface

The `datastore` kind supports the `document` interface type.

```yaml
kind: datastore
interface:
  type: document
  engine: mongodb
```

Allowed `engine` values for `type: document`:

- `mongodb`
- `couchbase`

## 4.3 Datastore Interface Fields

| Field | Required | Description |
| ----- | -------- | ----------- |
| `type` | YES | Datastore interface type |
| `engine` | NO | Specific datastore engine |

If `engine` is provided, it MUST be valid for the declared datastore interface type.

---

# 5. Cache Interfaces

Cache interfaces describe volatile key/value storage systems.

## 5.1 Key/Value Interface

The `cache` kind supports the `key_value` interface type.

```yaml
kind: cache
interface:
  type: key_value
  engine: redis
```

Allowed `engine` values for `type: key_value`:

- `redis`
- `memcached`

## 5.2 Cache Interface Fields

| Field | Required | Description |
| ----- | -------- | ----------- |
| `type` | YES | Cache interface type |
| `engine` | NO | Specific caching engine |

If `engine` is provided, it MUST be valid for the declared cache interface type.

---

# 6. Validation

A Condition that uses this extension is invalid if:

- `kind` is `api` and `interface.type` is not `http`
- `kind` is `datastore` and `interface.type` is not `relational` or `document`
- `kind` is `cache` and `interface.type` is not `key_value`
- `kind` is `api`, `interface.type` is `http`, and neither `spec` nor `operations` is present
- `kind` is `api`, `interface.type` is `http`, and `operations` is present but empty
- `kind` is `api`, `interface.type` is `http`, and any operation omits `method` or `path`
- `kind` is `api`, `interface.type` is `http`, and any operation declares an unsupported HTTP method
- `kind` is `api`, `interface.type` is `http`, and `spec` is present but omits `format` or `uri`
- `kind` is `api`, `interface.type` is `http`, and `spec.format` is not `openapi`
- `kind` is `api`, `interface.type` is `http`, and `spec.version` is present but is not a valid semantic version or supported version constraint expression
- `kind` is `datastore`, `interface.type` is `relational`, and `engine` is present but not one of the allowed relational engine values
- `kind` is `datastore`, `interface.type` is `document`, and `engine` is present but not one of the allowed document engine values
- `kind` is `cache`, `interface.type` is `key_value`, and `engine` is present but not one of the allowed key/value cache engine values

---

# 7. Example Profile

```yaml
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsProfile

metadata:
  name: checkout-service

workload:
  uri: https://github.com/example-org/checkout-service
  version: v1.2.3

extensions:
  - https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml

conditions:
  - name: primary-db
    kind: datastore
    interface:
      type: relational
      engine: postgres

  - name: request-cache
    kind: cache
    interface:
      type: key_value
      engine: redis

  - name: payments-api
    kind: api
    interface:
      type: http
      operations:
        - method: POST
          path: /charge
```

---

# 8. Summary

The Common Integrations extension defines common application integration vocabulary while leaving the core Runtime Conditions Profile focused on document structure, extension resolution, and deterministic validation.
