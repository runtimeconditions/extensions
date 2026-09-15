import copy
import sys
import unittest
from pathlib import Path

from jsonschema import Draft202012Validator


ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tools"))

from compile_extension import build  # noqa: E402
from serialization import read_document  # noqa: E402


class NATSServiceExtensionCompilationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.inventory = read_document(ROOT.parents[3] / "service-operations-inventories/nats/service-operations-inventory.yaml")
        cls.bridge = read_document(ROOT / "model/service-operations-semantic-bridge.yaml")
        cls.extension, cls.service_mapping = build(cls.inventory, cls.bridge)
        cls.validator = Draft202012Validator(cls.extension["spec"]["schemas"][0]["schema"])

    def assert_valid_operation(self, operation):
        condition = {"kind": "nats", "interface": {"type": "service", "operations": [operation]}}
        self.assertEqual(list(self.validator.iter_errors(condition)), [])

    def test_compiles_exact_extension_coordinates(self):
        self.assertEqual(self.extension["metadata"]["id"], self.service_mapping["extension"]["id"])
        self.assertEqual(self.extension["metadata"]["version"], self.service_mapping["extension"]["version"])
        self.assertEqual(self.extension["metadata"]["semanticSha256"], self.service_mapping["extension"]["semanticSha256"])

    def test_accepts_the_standard_inventory_document_contract(self):
        self.assertEqual(self.inventory["apiVersion"], "runtimeconditions.io/service-operations/v1alpha1")
        self.assertEqual(self.inventory["kind"], "RuntimeConditionsServiceOperationsInventory")
        self.assertEqual(self.inventory["metadata"]["name"], "nats.service")
        self.assertEqual(self.inventory["metadata"]["service"], "nats")
        self.assertNotIn("extension", self.inventory)
        self.assertEqual(self.bridge["kind"], "RuntimeConditionsServiceOperationsSemanticBridge")

    def test_generates_stable_language_neutral_service_mapping(self):
        self.assertEqual(self.service_mapping["kind"], "RuntimeConditionsServiceMapping")
        self.assertEqual(self.service_mapping["metadata"]["name"], self.inventory["metadata"]["name"])
        self.assertEqual(self.service_mapping["metadata"]["service"], self.inventory["metadata"]["service"])
        self.assertEqual(self.service_mapping["metadata"]["operationCount"], 26)
        operations = {item["name"]: item for item in self.service_mapping["operations"]}
        self.assertEqual(len(operations), 26)
        self.assertEqual(operations["stream.create"]["conditions"][0]["bindings"], {"required": ["name"], "optional": ["subjects"]})
        self.assertEqual(operations["stream.inspect"]["conditions"][0]["bindings"], {"required": ["name"], "optional": []})
        self.assertEqual(operations["consumer.consume"]["conditions"][0]["bindings"], {"required": ["stream"], "optional": ["name"]})
        for item in operations.values():
            condition = item["conditions"][0]
            bindings = condition["bindings"]
            operation = dict(condition["operation"])
            for field in bindings["required"]:
                operation[field] = ["value"] if field == "subjects" else "value"
            self.assert_valid_operation(operation)

    def test_rejects_operation_name_that_disagrees_with_fixed_semantics(self):
        inventory = copy.deepcopy(self.inventory)
        inventory["operations"][0]["name"] = "connection.publish"
        with self.assertRaisesRegex(ValueError, "resource.action"):
            build(inventory, self.bridge)

    def test_bridge_can_make_condition_requiredness_differ_from_service_requiredness(self):
        inventory_operation = next(item for item in self.inventory["operations"] if item["name"] == "consumer.delete")
        self.assertTrue(inventory_operation["inputs"]["consumerName"]["required"])
        mapped = next(item for item in self.service_mapping["operations"] if item["name"] == "consumer.delete")
        self.assertIn("name", mapped["conditions"][0]["bindings"]["optional"])

    def test_accepts_each_adapter_actionable_form(self):
        self.assert_valid_operation({"resource": "connection", "action": "connect"})
        self.assert_valid_operation({"resource": "subject", "action": "request", "subject": "inventory.reserve"})
        self.assert_valid_operation({"resource": "stream", "action": "create", "name": "ORDERS", "subjects": ["orders.>"]})
        self.assert_valid_operation({"resource": "stream", "action": "publish", "subject": "orders.created"})
        self.assert_valid_operation({"resource": "consumer", "action": "consume", "stream": "ORDERS", "name": "worker"})
        self.assert_valid_operation({"resource": "key_value", "action": "write", "bucket": "profiles"})
        self.assert_valid_operation({"resource": "object_store", "action": "watch", "bucket": "configuration"})

    def test_rejects_open_resource_action_combinations(self):
        condition = {"kind": "nats", "interface": {"type": "service", "operations": [{"resource": "subject", "action": "write", "subject": "orders.created"}]}}
        self.assertTrue(list(self.validator.iter_errors(condition)))

    def test_rejects_one_operation_with_multiple_actions(self):
        condition = {"kind": "nats", "interface": {"type": "service", "operations": [{"resource": "key_value", "action": ["read", "write"], "bucket": "profiles"}]}}
        self.assertTrue(list(self.validator.iter_errors(condition)))

    def test_rejects_fields_not_declared_for_one_operation(self):
        condition = {"kind": "nats", "interface": {"type": "service", "operations": [{"resource": "stream", "action": "inspect", "name": "ORDERS", "subjects": ["orders.>"]}]}}
        self.assertTrue(list(self.validator.iter_errors(condition)))


if __name__ == "__main__":
    unittest.main()
