import copy
import json
import sys
import tempfile
import unittest
from pathlib import Path


TOOLS = Path(__file__).resolve().parents[1] / "tools"
sys.path.insert(0, str(TOOLS))

from compile_extension import ModelDrift, build_outputs, operation_fingerprint  # noqa: E402


class SmithySemanticBridgeCompilationTest(unittest.TestCase):
    def model(self):
        return {
            "smithy": "2.0",
            "shapes": {
                "example#Service": {
                    "type": "service",
                    "version": "2026-01-01",
                    "operations": [{"target": "example#BucketCall"}, {"target": "example#ServiceCall"}],
                    "traits": {"aws.api#service": {"sdkId": "Example"}},
                },
                "example#BucketCall": {"type": "operation", "input": {"target": "example#Input"}},
                "example#ServiceCall": {"type": "operation", "input": {"target": "example#Input"}},
                "example#Input": {"type": "structure", "members": {"Bucket": {"target": "smithy.api#String"}}},
                "smithy.api#String": {"type": "string"},
            },
        }

    def bridge(self):
        names = ["BucketCall", "ServiceCall"]
        return {
            "apiVersion": "runtimeconditions.io/service-operations-semantic-bridge/v1alpha1",
            "kind": "RuntimeConditionsServiceOperationsSemanticBridge",
            "metadata": {"name": "example.service", "service": "example"},
            "operationSource": {
                "kind": "SmithyModel",
                "repository": "https://example.test/models.git",
                "path": "models/example.json",
                "serviceShape": "example#Service",
                "operationCount": len(names),
                "operationNamesSha256": operation_fingerprint(names),
            },
            "extension": {
                "id": "https://runtimeconditions.io/extensions/example/0.1.0/runtimeconditions.extension.yaml",
                "version": "0.1.0",
                "extensionName": "example",
                "serviceKey": "example",
                "displayName": "Example",
                "conditionKind": "example.service",
            },
            "defaultCondition": {"interfaceType": "bucket", "identityPath": "Bucket"},
            "operationMappings": [{"operation": "ServiceCall", "primaryCondition": {"interfaceType": "service"}}],
        }

    def compile(self, model, bridge):
        with tempfile.TemporaryDirectory() as directory:
            model_path = Path(directory, "model.json")
            model_path.write_text(json.dumps(model), encoding="utf-8")
            return build_outputs(model_path, "https://example.test/models.git", "revision", "models/example.json", "example#Service", model["shapes"], bridge)

    def test_bridge_generates_standalone_extension_and_service_mapping(self):
        extension, mapping, _ = self.compile(self.model(), self.bridge())
        self.assertNotIn("provenance", extension["metadata"])
        self.assertEqual(mapping["metadata"]["operationCount"], 2)
        self.assertEqual(mapping["operations"][0]["conditions"][0]["identity"]["path"], "Bucket")
        self.assertEqual(mapping["operations"][1]["conditions"][0]["interfaceType"], "service")
        self.assertIn("semanticBridgeSha256", mapping["metadata"])

    def test_unreviewed_source_operation_stops_generation(self):
        model = self.model()
        model["shapes"]["example#Service"]["operations"].append({"target": "example#NewCall"})
        model["shapes"]["example#NewCall"] = {"type": "operation", "input": {"target": "example#Input"}}
        with self.assertRaises(ModelDrift):
            self.compile(model, self.bridge())

    def test_bridge_cannot_map_an_operation_outside_the_service(self):
        bridge = copy.deepcopy(self.bridge())
        bridge["operationMappings"].append({"operation": "MissingCall", "primaryCondition": {"interfaceType": "service"}})
        with self.assertRaisesRegex(ModelDrift, "outside the service closure"):
            self.compile(self.model(), bridge)


if __name__ == "__main__":
    unittest.main()
