# Phase 3 Todo List: Python Emitter

This checklist tracks the design decisions and implementation work required for
Phase 3. It is based on the Phase 1 and Phase 2 implementation records and the
current Python emitter requirements in `IMPLEMENTATION.md`.

`IMPLEMENTATION.md` is the implementation authority. This file is a working
checklist and does not replace or amend that document.

## 1. Resolve the Phase 3 contracts

- [ ] Decide how Python binding manifests relate to the current Go-specific
      provisional manifest schema.
- [ ] Define the Python package-target schema.
- [ ] Define the Python distribution name versus import-package name.
- [ ] Define the generated Python source/package layout.
- [ ] Define the minimum supported Python version.
- [ ] Resolve the `StrEnum` requirement versus Python 3.10 support.
- [ ] Resolve the repository path inconsistency between
      `tooling/extension-bindings/` and
      `extensions/tooling/extension-bindings/`.
- [ ] Resolve the older `runtimeconditions.package.yaml` convention versus the
      Phase 3 binding resources required by `IMPLEMENTATION.md`.

## 2. Define the Python type-system mapping

- [ ] Define Python representations for string, boolean, integer, number, null,
      and `any` structural shapes.
- [ ] Define Python representations for heterogeneous unions.
- [ ] Define recursive object and collection type generation.
- [ ] Define recursive type-alias syntax compatible with the minimum Python
      version and `mypy --strict`.
- [ ] Define map key typing explicitly; JSON object keys should normally be
      represented as `str`.
- [ ] Define how required nullable fields differ from omitted optional fields.
- [ ] Decide whether the prescribed `T | None = None` representation is
      sufficient for all omission/nullability cases.
- [ ] Define the exact marker protocol and marker attribute contract.
- [ ] Define how scalar, enum, collection, map, union, and object values satisfy
      the applicable kind protocols.
- [ ] Define cross-package additive protocol implementation and imports.

## 3. Define Python naming and collision behavior

- [ ] Define the Python-specific ordering of source-name decomposition, case
      conversion, keyword escaping, numeric-prefix handling, and dunder-name
      handling.
- [ ] Define Python reserved-word and soft-keyword handling.
- [ ] Define handling of names that shadow built-ins or imported helper names.
- [ ] Define generated module and package naming rules.
- [ ] Define package-level fixed symbols and collision behavior for Python.
- [ ] Define collision behavior for generated classes, aliases, protocols,
      functions, fields, enum classes, and enum members.
- [ ] Define the deterministic diagnostic format and error codes for Python
      naming failures.
- [ ] Add Python-specific negative fixtures for relevant collision cases.

## 4. Define generated Python source structure

- [ ] Decide whether generated declarations live in one module or multiple
      modules.
- [ ] Define package `__init__.py` exports and the `__all__` policy.
- [ ] Define the generated `Declaration` class and inert declaration function
      behavior.
- [ ] Define frozen keyword-only dataclass details, including field ordering,
      defaults, and forward references.
- [ ] Define enum member generation with explicit serialized values.
- [ ] Define generated type-alias syntax and whether aliases are public API.
- [ ] Define the standard generated-source header for Python files and
      `pyproject.toml`.

## 5. Define Python package metadata and resources

- [ ] Define deterministic `pyproject.toml` generation.
- [ ] Define build backend and pinned build-tool versions.
- [ ] Define runtime dependency metadata for direct Python binding dependencies.
- [ ] Apply the required dependency interval rule:
      `>=<tested>,<next-breaking>`.
- [ ] Define `Requires-Python` generation and validation.
- [ ] Define license, repository URL, and publication metadata generation.
- [ ] Define package-data configuration for Runtime Conditions YAML resources.
- [ ] Verify the required resources through `importlib.resources` from an
      installed distribution.
- [ ] Define whether conformance source is included in wheels, source
      distributions, or both.

## 6. Define archive and reproducibility behavior

- [ ] Define the exact Phase 3 scope of wheel and source-distribution
      verification.
- [ ] Decide whether wheel and source-distribution bytes must be reproducible,
      not merely their contents.
- [ ] Define timestamp, file-order, ownership, permissions, compression, and
      `SOURCE_DATE_EPOCH` handling.
- [ ] Define archive file manifests and undeclared-file verification.
- [ ] Define how Phase 3 verifies resources that the later orchestrator adds.
- [ ] Verify wheels and source distributions in clean isolated environments.
- [ ] Verify package dependencies through the native Python package manager,
      not filesystem or cache scans.

## 7. Implement the Python emitter

- [ ] Add the Python emitter project under the planned emitter layout.
- [ ] Implement strict model and package-target loading.
- [ ] Implement safe YAML parsing with duplicate-key rejection and the required
      input limits.
- [ ] Implement Python naming and complete-set collision allocation.
- [ ] Implement declaration functions and marker protocols.
- [ ] Implement dataclasses, scalar types, enums, aliases, arrays, maps, unions,
      references, and recursive shapes.
- [ ] Implement direct and transitive dependency imports.
- [ ] Implement binding-manifest generation.
- [ ] Implement deterministic `pyproject.toml` generation.
- [ ] Implement the command-line entry point and standard emitter contract.
- [ ] Implement wheel and source-distribution creation/inspection helpers.

## 8. Add Python conformance coverage

- [ ] Emit every positive language-neutral conformance model.
- [ ] Cover owned declarations and interface objects.
- [ ] Cover direct additive fields.
- [ ] Cover transitive dependency closure.
- [ ] Cover recursive references.
- [ ] Cover object-only alternatives with branch-dependent requiredness.
- [ ] Cover heterogeneous unions.
- [ ] Cover scalar arrays, object arrays, and schema-valued maps.
- [ ] Cover scoped value domains and normalized-name collisions.
- [ ] Cover reserved words, case boundaries, language-specific leading-digit
      behavior, and non-ASCII naming cases.
- [ ] Cover every generated declaration, type, field, enum member, collection,
      map, and union in conformance source.
- [ ] Add exact expected Python diagnostics for every Python-relevant negative
      model.

## 9. Reproduce the applicable Go emitter gates for Python

- [ ] Gate 1: validate the normalized model schema.
- [ ] Gate 2: validate the Python binding manifest schema.
- [ ] Gate 3: parse Python package metadata with the native package tooling.
- [ ] Gate 4: pass `ruff format --check`.
- [ ] Gate 5: compile with the minimum supported Python version.
- [ ] Gate 6: pass `mypy --strict`.
- [ ] Gate 7: pass the native Python test suite.
- [ ] Gate 8: verify complete conformance-source coverage.
- [ ] Gate 12: verify deterministic repeated emission.
- [ ] Gate 14: reject undeclared archive files.
- [ ] Gate 16: compare the exported Python API surface using the native AST
      parser with the mechanically derived model API.
- [ ] Add direct additive package composition tests using Python source
      packages.
- [ ] Add transitive additive package composition tests using Python source
      packages.
- [ ] Add the same composition tests against built and installed wheel
      artifacts.
- [ ] Verify package-data resources from installed wheels and source
      distributions.

## 10. Phase 3 exit review

- [ ] Confirm no extension YAML was modified.
- [ ] Confirm no emitter behavior is keyed to a conformance identifier,
      extension identifier, kind, field, or value.
- [ ] Confirm generated source and metadata contain no timestamps or
      host-specific paths.
- [ ] Confirm all applicable diagnostics are deterministic and exact.
- [ ] Confirm the final Phase 3 file list is approved before implementation if
      it exceeds five files.
- [ ] Record Phase 3 verification results in `PHASE-3.md`.
