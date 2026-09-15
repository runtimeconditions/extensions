import sys
import unittest
from pathlib import Path

from jsonschema import Draft202012Validator


ROOT = Path(__file__).resolve().parents[1]
TOOLS = ROOT / "tools"
sys.path.insert(0, str(TOOLS))

from compile_extension import build_outputs, profile_operation, resource_families  # noqa: E402
from serialization import read_document  # noqa: E402


class KubernetesExtensionCompilationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source_projection = read_document(ROOT / "model/generated/kubernetes-v1.36-openapi-projection.yaml")
        cls.bridge = read_document(ROOT / "model/service-operations-semantic-bridge.yaml")
        cls.extension, cls.mapping = build_outputs(cls.source_projection, cls.bridge)
        cls.validator = Draft202012Validator(cls.extension["spec"]["schemas"][0]["schema"])

    def test_compiles_every_authoritative_operation(self):
        self.assertEqual(self.mapping["metadata"]["operationCount"], 1123)
        self.assertEqual(len(self.mapping["operations"]), 1123)
        self.assertEqual(self.mapping["metadata"]["resourceCount"], 95)
        self.assertEqual(len(self.mapping["resources"]), 95)
        self.assertEqual(self.mapping["extension"]["semanticSha256"], self.extension["metadata"]["semanticSha256"])

    def test_discovery_catalog_maps_one_gvk_to_one_built_in_resource(self):
        config_map = next(item for item in self.mapping["resources"] if item["apiGroup"] == "" and item["apiVersion"] == "v1" and item["kind"] == "ConfigMap")
        self.assertEqual(config_map["resource"], "configmaps")
        self.assertTrue(config_map["namespaced"])
        self.assertEqual(next(item for item in config_map["operations"] if item["verb"] == "list")["scopes"], ["all_namespaces", "namespaced"])
        selectors = [(item["apiGroup"], item["apiVersion"], item["kind"]) for item in self.mapping["resources"]]
        self.assertEqual(len(selectors), len(set(selectors)))

    def test_config_map_operation_matches_approved_shape(self):
        operation = next(item for item in self.mapping["operations"] if item["name"] == "readCoreV1NamespacedConfigMap")
        self.assertEqual(
            operation["conditions"][0]["operation"],
            {"verb": "get", "apiGroup": "", "apiVersion": "v1", "resource": "configmaps", "scope": "namespaced"},
        )

    def test_connect_preserves_http_method(self):
        source = next(item for item in self.source_projection["operations"] if item["operationId"] == "connectCoreV1PostNamespacedPodExec")
        operation = profile_operation(source, self.bridge, resource_families(self.source_projection))
        self.assertEqual(operation["verb"], "connect")
        self.assertEqual(operation["method"], "post")

    def test_schema_accepts_crd_coordinates_but_rejects_combinatorial_operation(self):
        valid = {
            "kind": "kubernetes",
            "interface": {
                "type": "api",
                "operations": [{"verb": "patch", "apiGroup": "widgets.example.io", "apiVersion": "v1alpha1", "resource": "widgets", "scope": "namespaced", "subresource": "status"}],
            },
        }
        self.assertEqual(list(self.validator.iter_errors(valid)), [])
        invalid = {
            "kind": "kubernetes",
            "interface": {
                "type": "api",
                "operations": [{"verb": ["get", "patch"], "apiGroup": "widgets.example.io", "apiVersion": "v1", "resource": "widgets", "scope": ["cluster", "namespaced"]}],
            },
        }
        self.assertTrue(list(self.validator.iter_errors(invalid)))


if __name__ == "__main__":
    unittest.main()
