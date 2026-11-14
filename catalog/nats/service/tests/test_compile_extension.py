import copy
import sys
import unittest
from pathlib import Path

from jsonschema import Draft202012Validator


ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tools"))

from compile_extension import build, environment_configuration_schema  # noqa: E402
from serialization import read_document  # noqa: E402


class NATSServiceExtensionCompilationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.inventory = read_document(ROOT.parents[3] / "service-operations-inventories/nats/service-operations-inventory.yaml")
        cls.bridge = read_document(ROOT / "model/service-operations-semantic-bridge.yaml")
        cls.extension, cls.service_mapping = build(cls.inventory, cls.bridge)
        cls.validator = Draft202012Validator(cls.extension["spec"]["schemas"][0]["schema"])

    def assert_valid_operation(self, operation):
        condition = {"kind": "nats.service", "interface": {"type": "service", "operations": [operation]}}
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
        condition = {"kind": "nats.service", "interface": {"type": "service", "operations": [{"resource": "subject", "action": "write", "subject": "orders.created"}]}}
        self.assertTrue(list(self.validator.iter_errors(condition)))

    def test_rejects_one_operation_with_multiple_actions(self):
        condition = {"kind": "nats.service", "interface": {"type": "service", "operations": [{"resource": "key_value", "action": ["read", "write"], "bucket": "profiles"}]}}
        self.assertTrue(list(self.validator.iter_errors(condition)))

    def test_rejects_fields_not_declared_for_one_operation(self):
        condition = {"kind": "nats.service", "interface": {"type": "service", "operations": [{"resource": "stream", "action": "inspect", "name": "ORDERS", "subjects": ["orders.>"]}]}}
        self.assertTrue(list(self.validator.iter_errors(condition)))

    def test_nats_owns_configuration_without_extension_dependencies(self):
        self.assertEqual(self.extension["metadata"]["version"], "0.2.0")
        self.assertFalse(self.extension["spec"].get("dependencies"))
        self.assertEqual(self.extension["spec"]["conditionFields"], [
            {"name": "configuration", "appliesToKinds": ["nats.service"], "appliesToInterfaceTypes": ["service"]},
        ])
        schemas = [entry for entry in self.extension["spec"]["schemas"] if entry["id"] == "nats-service-configuration"]
        self.assertEqual(len(schemas), 1)
        domains = [entry for entry in self.extension["spec"]["fieldValues"] if entry["field"].startswith("configuration.")]
        self.assertEqual(len(domains), 2)
        for domain in domains:
            self.assertEqual(domain["values"], self.bridge["extension"]["environmentConfiguration"]["envProperties"])

    def test_env_configuration_keeps_only_its_common_dependency(self):
        catalog = ROOT.parents[1]
        env = read_document(catalog / "rc/env-configuration/env-configuration-v1alpha2.yaml")
        self.assertEqual(env["spec"]["dependencies"], [
            "https://runtimeconditions.io/extensions/rc/common-integrations/v1alpha2/runtimeconditions.extension.yaml",
        ])
        for group, field in (("conditionFields", "appliesToKinds"), ("fieldValues", "targetKind"), ("schemas", "appliesToKind")):
            for entry in env["spec"][group]:
                self.assertNotIn("nats.service", entry.get(field, []))
        self.assertTrue(any(entry.get("appliesToInterfaceType") == "smtp" for entry in env["spec"]["schemas"]))

    def test_smtp_configuration_validates_with_only_common_integrations(self):
        catalog = ROOT.parents[1]
        common = read_document(catalog / "rc/common-integrations/common-integrations-v1alpha2.yaml")
        env = read_document(catalog / "rc/env-configuration/env-configuration-v1alpha2.yaml")
        self.assertFalse(common["spec"].get("dependencies"))
        condition = {
            "kind": "api", "interface": {"type": "smtp", "operations": ["send"]},
            "configuration": {"env": [{"property": "hostname", "name": "SMTP_HOST"}]},
        }
        for extension in (common, env):
            for entry in extension["spec"]["schemas"]:
                if entry.get("appliesToKind", "api") == "api" and entry.get("appliesToInterfaceType", "smtp") == "smtp":
                    with self.subTest(extension=extension["metadata"]["id"], schema=entry["id"]):
                        self.assertEqual(list(Draft202012Validator(entry["schema"]).iter_errors(condition)), [])

    def test_corrected_documents_match_the_extension_authoring_schema(self):
        catalog = ROOT.parents[1]
        schema = read_document(catalog.parent / "tooling/extension-bindings/model/runtimeconditions.extension-semantic.schema.yaml")
        validator = Draft202012Validator(schema)
        env = read_document(catalog / "rc/env-configuration/env-configuration-v1alpha2.yaml")
        for extension in (env, self.extension):
            with self.subTest(extension=extension["metadata"]["id"]):
                self.assertEqual(list(validator.iter_errors(extension)), [])

    def test_nats_configuration_validates_without_env_configuration(self):
        schema = environment_configuration_schema(self.bridge["extension"]["environmentConfiguration"])
        validator = Draft202012Validator(schema)
        self.assertEqual(list(validator.iter_errors({})), [])
        for prop in self.bridge["extension"]["environmentConfiguration"]["envProperties"]:
            entry = {"property": prop, "name": "NATS_INPUT", "sensitive": True, "required": False}
            for config in ({"env": [entry]}, {"alternatives": [{"env": [entry]}, {"env": [entry]}]}):
                with self.subTest(property=prop, configuration=config):
                    self.assertEqual(list(validator.iter_errors({"configuration": config})), [])

    def test_nats_configuration_rejects_invalid_inputs(self):
        validator = Draft202012Validator(environment_configuration_schema(self.bridge["extension"]["environmentConfiguration"]))
        valid = {"property": "url", "name": "NATS_URL"}
        invalid = [
            {}, {"env": []}, {"alternatives": [{"env": [valid]}]},
            {"env": [valid], "alternatives": [{"env": [valid]}, {"env": [valid]}]},
            {"env": [{"property": "database", "name": "DB"}]},
            {"env": [{"property": "url", "name": "bad-name"}]},
            {"env": [{**valid, "sensitive": "true"}]},
            {"env": [{**valid, "required": "false"}]},
            {"env": [{**valid, "value": "not-a-declaration-field"}]},
        ]
        for config in invalid:
            with self.subTest(configuration=config):
                self.assertTrue(list(validator.iter_errors({"configuration": config})))

    def test_rejects_invalid_configuration_authoring(self):
        for config in (None, {}, {"envProperties": []}, {"envProperties": "url"},
                       {"envProperties": ["url", "url"]}, {"envProperties": [""]},
                       {"envProperties": [None]}, {"envProperties": ["url"], "unknown": True}):
            with self.subTest(configuration=config):
                with self.assertRaisesRegex(ValueError, "environmentConfiguration"):
                    environment_configuration_schema(config)

    def test_legacy_inputs_preserve_the_existing_release_in_memory(self):
        bridge = copy.deepcopy(self.bridge)
        del bridge["extension"]["environmentConfiguration"]
        bridge["extension"].update(
            id="https://runtimeconditions.io/extensions/nats/service/0.1.0/runtimeconditions.extension.yaml", version="0.1.0",
        )
        extension, mapping = build(self.inventory, bridge)
        self.assertEqual(extension, read_document(ROOT / "releases/0.1.0/runtimeconditions.extension.yaml"))
        self.assertEqual(mapping["metadata"]["operationCount"], self.service_mapping["metadata"]["operationCount"])
        self.assertEqual(mapping["operations"], self.service_mapping["operations"])

    def test_committed_service_mapping_matches_the_compiler_output(self):
        self.assertEqual(self.service_mapping, read_document(ROOT / "model/generated/nats-service-mapping.yaml"))


if __name__ == "__main__":
    unittest.main()
