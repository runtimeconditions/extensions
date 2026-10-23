# Contributing

## Layout

`catalog/` holds the extensions, `tooling/` holds the automation that checks and
turns them into language bindings, and `bindings/` holds the generated
packages. The [README](README.md) describes each part, and
[`EXTENSION_RELEASES.md`](EXTENSION_RELEASES.md) describes how extension
identifiers stay immutable.

`bindings/` is written by `binding-generate.yml`, which commits to `main` as
`chore(bindings): regenerate ...`. Never edit or commit files there by hand; change
the emitter, the catalog entry, or the extension instead.

## Running the checks

CI runs `tests.yml` and the emitter jobs in `extension-binding-normalizer.yml`.
To run the same checks locally, use Python 3.12 with the pinned tooling and Go
as pinned in each `go.mod`:

```sh
pip install --no-deps -r tooling/extension-bindings/ci/python-tooling-requirements.txt
python -m unittest discover -s tooling/common -p 'test_*.py'
python -m unittest discover -s tooling/extension-bindings/ci -p 'test_*.py'
PYTHONPATH=tooling/extension-bindings/emitters/python/src python -m pytest -q \
  tooling/extension-bindings/emitters/python/tests \
  --ignore=tooling/extension-bindings/emitters/python/tests/test_packages.py
go -C tooling/extension-bindings/orchestrator test ./...
go -C tooling/extension-bindings/normalizer test ./...
go -C tooling/extension-bindings/emitters/go test ./...
npm --prefix tooling/extension-bindings/emitters/typescript test
```

The NATS tests read the sibling checkout
`../service-operations-inventories`; clone
`runtimeconditions/service-operations-inventories` next to this repository before
running `pytest catalog/nats/service/tests`.

## Pull requests

- Keep one change per pull request, with a short title written as an imperative
  sentence, such as `Add a Java binding emitter`.
- Use the description template: what changed and why, how you tested it, and what
  is not covered. Say plainly when something was not run.
- Mention any change to a published identifier, schema, or generated file layout,
  since consumers depend on them.

## Adding an extension

1. Add `catalog/<namespace>/<name>/` with its release under `releases/<version>/`.
   A published identifier is immutable, so a new semantic definition gets a new
   version.
2. Keep every `kind` in the extension's namespace; `enforce-kind-namespacing.yml`
   rejects the rest.
3. Add the extension to the table in the README and, to generate bindings, to
   `tooling/extension-bindings/packages.yaml`.

## Adding a binding emitter

An emitter lives in `tooling/extension-bindings/emitters/<language>/` and should
bring:

- a `CONTRACT.md` with the package target, generated layout, naming rules, and
  diagnostic codes, plus a `package-target.schema.yaml`;
- `emit`, `verify`, and `api` commands, where `verify` re-renders and compares;
- tests that run the models in `model/conformance/expected` and the committed
  first-party models, check byte-identical output, compile or load the generated
  package, and cover negative inputs with exact diagnostics;
- a verify job in `extension-binding-normalizer.yml` and an entry under
  `languages` in `toolchain.lock.yaml`;
- manifest and release schema support for the new language, and a catalog entry
  in `packages.yaml` once the language is generated in CI.
