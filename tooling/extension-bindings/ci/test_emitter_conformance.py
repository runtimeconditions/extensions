"""Keep the shared emitter fixtures complete and the projection stable."""

from __future__ import annotations

import unittest

import emitter_conformance as conformance


class FixtureTests(unittest.TestCase):
    def test_every_positive_case_has_a_neutral_manifest_fixture(self) -> None:
        models = {
            path.parent.name
            for path in conformance.EXPECTED.glob("*/runtimeconditions.binding-model.yaml")
        }
        fixtures = {
            path.parent.name for path in conformance.EXPECTED.glob("*/manifest-neutral.yaml")
        }
        self.assertTrue(models)
        self.assertEqual(models, fixtures)

    def test_projection_keeps_only_the_language_neutral_fields(self) -> None:
        manifest = {
            "declarations": [{"modelRef": {"coordinate": "a"}, "owner": "o", "sourceName": "s", "function": "f"}],
            "rootBindings": [
                {"role": "condition-field", "sourceName": "x", "value": {"type": "T"}, "nativeName": "n"}
            ],
        }
        self.assertEqual(
            conformance.project(manifest),
            {
                "declarations": [{"modelRef": {"coordinate": "a"}, "owner": "o", "sourceName": "s"}],
                "importedMarkerContracts": [],
                "rootBindings": [{"role": "condition-field", "sourceName": "x"}],
            },
        )

    def test_projection_ignores_order(self) -> None:
        first = {"rootBindings": [{"role": "a", "sourceName": "1"}, {"role": "b", "sourceName": "2"}]}
        second = {"rootBindings": list(reversed(first["rootBindings"]))}
        self.assertEqual(conformance.project(first), conformance.project(second))

    def test_direct_dependencies_come_from_the_root_extension(self) -> None:
        model = {
            "rootExtension": {"id": "root"},
            "extensions": [
                {"id": "leaf"},
                {"id": "root", "dependencies": ["b", "a"]},
            ],
        }
        self.assertEqual(conformance.direct_dependencies(model), ["a", "b"])


if __name__ == "__main__":
    unittest.main()
