# Rust emitter contract

This records the Rust target, layout, and naming decisions for the native Rust
emitter. It is written in Rust, as the implementation standard requires of every
emitter, and follows the Python emitter's structure.

```text
runtimeconditions-binding-emitter-rust emit \
  --model runtimeconditions.binding-model.yaml \
  --package-config <target.yaml> --output <new empty directory>
```

`verify --model --package-config --input <package>` parses the generated source
with `syn` (never compiling or running it), re-renders the package from the
model, and requires every file to equal the fresh rendering. It prints the
structural signatures and a mapping summary. `api --input <package>` prints the
structural signatures only.

## Target configuration

The selected target uses `runtimeconditions.io/rust-package-target/v1alpha1` and
`RuntimeConditionsRustPackageTarget`, defined by `package-target.schema.yaml`.

- `packageKey`, `rootExtension`, `version`, `sourceDirectory`, and
  `publicationMode` correspond to the package set and language entry in the
  catalog. `sourceDirectory` is exactly `bindings/<packageKey>/rust`.
- `crateName` is the Cargo package name. Its library identifier replaces `-`
  with `_`. It is never inferred from extension vocabulary.
- `minimumRustVersion` is `<major>.<minor>` and at least `1.70`. It becomes
  `rust-version`.
- `repositoryUrl` must use HTTPS. A registry target additionally requires
  `registryId`.
- `dependencies` contains only direct extension dependencies, with their crate
  names and exact tested versions, and equals the model's direct dependency set.
- `emitterSha256` is supplied by the caller and recorded in the manifest.

Package versions are SemVer without a leading `v` or build metadata. Supported
prerelease forms are `-alpha.N`, `-beta.N`, and `-rc.N`. Direct dependency
requirements use `>=<tested>, <next-breaking>`, where `next-breaking` is
`0.(y+1).0` below `1.0.0` and `(major+1).0.0` otherwise.

## Generated source tree

```text
<new output directory>/
  Cargo.toml
  src/lib.rs
  src/bindings.rs
  src/bindings/<module>.rs
  runtimeconditions/runtimeconditions.bindings.yaml
```

`lib.rs` re-exports `bindings.rs`, which declares one module per type, re-exports each
type, and holds `Declaration`, the marker traits, and the declaration functions. A type
module is the snake-case form of the type name; a name that is not ASCII is stored in
`module_<16 hex digits>.rs` and declared with `#[path]`. Every type module starts with
`use super::*;`. `serde_json` is a dependency only when the model uses a JSON value.
The `serde` feature adds `Serialize` and `Deserialize` derives: fields and values keep
their source names, optional fields are skipped when absent, unions are untagged, and
declaration-field wrappers are transparent. Generated source is formatted by `rustfmt` (edition 2021, Unix newlines) from the pinned
toolchain at emission time and starts with the non-editable header
naming the model digest and emitter version; `Cargo.toml` carries it as a `#`
comment.

## API rules

1. Every owned kind becomes one function `kind(&[&dyn KindField]) -> Declaration`.
   `Declaration` is an inert tuple struct with a private field.
2. A crate that imports marker traits re-exports them from `bindings.rs` with `pub use`, so a
   consumer that depends only on the middle package of a chain can import a trait owned by a
   package further down.
3. `KindField` is a trait whose only method is `rc_marker_<16 hex digits>`,
   derived from the owner and declaration coordinate. Every applicable struct
   implements it, so a field belonging to another kind does not compile.
   Cross-package additive fields implement the provider's trait, imported with
   `use <crate>::<Trait>`.
4. Objects become structs with public fields, required fields first. An
   optional field is `Option<T>`. The emitter does not add constructors or
   builders.
5. Scalars map to `String`, `bool`, `i64`, `f64`, `()` for JSON null, and
   `serde_json::Value` for any JSON value.
6. Arrays and maps become tuple structs around `Vec<T>` and
   `BTreeMap<String, T>`. Named wrappers keep recursive shapes expressible.
7. Unions become enums with variants `Variant1`, `Variant2`, ...
8. String value domains become enums whose variants are Pascal case and whose
   `value()` returns the serialized string.
9. A direct reference is boxed only when it lies on a cycle of direct references.
   References through `Vec` and `BTreeMap` already have a finite size.
10. Every type, field, and value carries a doc comment naming its source and the
   coordinate and JSON Pointer it was generated from.

Source names are NFKC-normalized before they are split into words. Types and traits
use Pascal case; fields and functions use snake case. A leading digit gains `X`
(types) or `x`. A Rust keyword becomes a raw identifier (`r#type`); the keywords that
cannot be raw (`self`, `Self`, `super`, `crate`, `_`) gain one trailing underscore. A
name that equals a prelude or generated helper (`Option`, `Vec`, `Declaration`,
...) stops emission instead of being renamed. Collisions are resolved only by
prepending parent path components, nearest first, and otherwise stop emission.

## Diagnostics

Every error has a category, a code, a coordinate, an optional JSON Pointer into the
input that failed, and a message. By default it prints as
`<code> <coordinate> <pointer>: <message>`. With `--json` it prints one JSON object
on stderr: `{"error": {"category", "code", "coordinate", "jsonPointer", "message"}}`.
Categories are `usage`, `input`, `model`, `package-config`, `rust-naming`, `output`,
and `verification`. Target errors point at the failing field, such as `/version`;
schema failures point at the first failing instance location; model shape errors
use the provenance coordinate and JSON Pointer.

| Code | Meaning |
| --- | --- |
| `RCR0001` | Invalid command line. |
| `RCR1001` | YAML input cannot be read or exceeds the size limit. |
| `RCR1002` | YAML syntax, profile, structure, or resource-limit failure. |
| `RCR1003` | Binding-model schema validation failure. |
| `RCR1004` | Unsupported binding-model `apiVersion`. |
| `RCR1005` | Unsupported binding-model `kind`. |
| `RCR1006` | Invalid Rust package-target field or dependency metadata. |
| `RCR1007` | Target source directory does not match its package key. |
| `RCR1008` | Unsupported minimum Rust version. |
| `RCR1009` | Target root extension or direct dependency set differs from the model. |
| `RCR1010` | Unknown structural shape kind. |
| `RCR1011` | Inconsistent extension closure or declaration ownership. |
| `RCR1012` | Inconsistent shape details, value domain, or unresolved reference. |
| `RCR2000` | A source name cannot be derived as a valid Rust identifier. |
| `RCR2001` | Symbol collision or reserved-name conflict. |
| `RCR3001` | Output directory is not new or empty. |
| `RCR3002` | Output directory cannot be inspected or written. |
| `RCR4001` | Generated source cannot be parsed or has a non-public item. |
| `RCR4002` | Generated package differs from a fresh rendering. |
| `RCR4003` | `rustfmt` is unavailable or rejects the generated source. |

## Input loading

The model and target are read as UTF-8 without BOM, LF line endings, one
trailing newline, no comments, anchors, aliases, or tags, string mapping keys
only, no duplicate keys, finite numbers, and bounded size, node count, and depth. At most
64 MiB plus one byte of a file is read before it is rejected.
The model is then validated against
`model/runtimeconditions.binding-model.schema.yaml`, embedded at build time;
`RC_BINDING_MODEL_SCHEMA` overrides it.

## Verification

`cargo test` runs the shared conformance models in `model/conformance/expected`.
For each it checks that three emissions are byte-identical, that `verify`
accepts the output, that the source is `rustfmt --check` clean, that every crate passes `cargo check` with and without `--features serde`, that dependency-free crates round-trip every constructed value through JSON, that the manifest satisfies the binding-manifest schema, and
that the crate compiles with `rustc -D warnings` when it has no dependencies.
Tampered output and invalid inputs must fail with the codes above.

## Orchestration

`rc bindings` drives the emitter as an external binary, built with
`cargo build --release --locked` unless `--rust-emitter` or
`RC_BINDINGS_RUST_EMITTER` names an installed one. The catalog entry's
`coordinate` is the crate name, `name` its library identifier, and
`languageVersion` the exact Rust release; `publicationMode` is `github-tag`
until a registry workflow exists.

- The emitter's version is the crate version in `Cargo.toml`, reported by `--version`. Its
  identity digest hashes `Cargo.toml`, `Cargo.lock`, the target schema, and `src/*.rs`, not the
  binary, so every platform records the same `emitterSha256` and regenerating is reproducible.
- Assembly writes the model, extension, and release resources into
  `runtimeconditions/` and the file manifest at the crate root. `verify`
  ignores exactly those four files.
- The release manifest records no profiler, because no independent Rust
  profiler exists. The release schema forbids a profiler for Rust and requires
  one for Go and Python.
- Native verification requires `rustc` at exactly the catalog version, a
  `rustfmt --check`, and `cargo check` of a staged copy whose direct
  dependencies point at the staged provider crates.
- The package archive is a deterministic zip of the crate tree, recorded as
  `rust-source-zip`. Registry publication is a placeholder workflow.

## Design notes

- **Functions.** The declaration functions are free functions in `bindings.rs`;
  the manifest names that file.
- **Build tooling.** The emitter and the generated crate use Cargo. Tool
  versions are recorded under `languages.rust` in `toolchain.lock.yaml`.
