# Conformance fixtures

Every native emitter is checked against the same inputs.

- `cases/<case>/` holds the extension definitions for one scenario. `root.yaml` is the
  extension under test; other files in the folder are its dependencies.
- `expected/<case>/runtimeconditions.binding-model.yaml` is the normalized binding model
  for a positive case, and `expected/<case>/diagnostic.yaml` is the diagnostic for a
  negative one. The normalizer test regenerates and compares them.
- `expected/<case>/manifest-neutral.yaml` is the part of the binding manifest that comes
  from the model rather than the target language: the declarations, the imported marker
  contracts, and the root bindings (role, model reference, scope, path, and schema and
  vocabulary coordinates). Native names, types, and files are left out because they
  depend on the language.

| Case | What it covers |
| --- | --- |
| `01`, `02`, `03` | Owned kinds and interfaces, additive fields across packages, transitive closure |
| `04`, `05`, `12` | Negative: dependency cycle, vocabulary conflict, unsupported structural keyword |
| `06`, `07`, `08`, `09` | Recursive references, object alternatives, unions, collections and maps |
| `10`, `11` | Scoped domain collisions, source names that must survive naming |
| `13` | A dependency that contributes only a schema |
| `14` | Properties that reference scalar, unconstrained, enum, and chained definitions |
| `15` | Property names that are reserved words in common languages |

## Checking an emitter

`ci/emitter_conformance.py` generates every positive case with each emitter, projects the
generated manifest onto the neutral fields, and compares it with the fixture:

```sh
python tooling/extension-bindings/ci/emitter_conformance.py --emitters python,typescript,rust
```

The TypeScript emitter must be built first; the Rust emitter is built by the script. A new
emitter joins by adding a small adapter class that writes its package target and says
where its manifest lands. The Go emitter is not included: its manifest predates the
current shape and uses vocabulary coordinates for its model references and leaves out the
schema and vocabulary coordinates, and it rejects unconstrained definitions (case 14) with
`RCG1024`.

## Adding a case

1. Add `cases/<NN>-<name>/root.yaml` and append the case to `conformanceCases` in
   `normalizer/conformance_test.go`.
2. Run `UPDATE_CONFORMANCE=1 go test -run TestConformanceSuite ./...` in `normalizer/` and
   review the new `expected/` files.
3. Run `python tooling/extension-bindings/ci/emitter_conformance.py --emitters python --update`
   to write the neutral fixture, then check the other emitters against it. Emitters that
   discover cases by directory, like the Python, TypeScript, and Rust test suites, pick the
   new case up automatically.
