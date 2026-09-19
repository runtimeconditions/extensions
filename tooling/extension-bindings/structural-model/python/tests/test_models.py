from __future__ import annotations

import unittest

import common_integrations as common
import env_configuration as env


class StructuralModelTest(unittest.TestCase):
    def test_common_and_environment_configuration_shapes_compose(self) -> None:
        common.api(
            common.Http(
                spec=common.Spec(uri="https://example.test/openapi.yaml"),
                operations=(
                    common.OperationsItem(
                        method=common.HTTPMethod.GET,
                        path="/todos",
                        response_schema={"id": common.SchemaString},
                    ),
                ),
            ),
            env.Configuration(
                env=(
                    env.Env(property=env.Property.BASE_URL, name="TODOS_API_URL"),
                    env.Env(property=env.Property.TOKEN, name="TODOS_API_TOKEN"),
                ),
            ),
        )

        common.datastore(
            common.Relational(engine=common.RelationalEngine.POSTGRES),
            env.Configuration(
                env=(
                    env.Env(property=env.Property.DATABASE, name="DATABASE_NAME"),
                ),
            ),
        )

        common.cache(
            common.KeyValue(engine=common.KeyValueEngine.REDIS),
            env.Configuration(
                alternatives=(
                    env.AlternativesItem(
                        env=(env.Env(property=env.Property.URL, name="REDIS_URL"),),
                    ),
                    env.AlternativesItem(
                        env=(
                            env.Env(property=env.Property.HOSTNAME, name="REDIS_HOST"),
                            env.Env(property=env.Property.PORT, name="REDIS_PORT"),
                        ),
                    ),
                ),
            ),
        )

    def test_schema_variants_are_native_values(self) -> None:
        schema: common.SimpleSchema = {
            "id": common.SchemaString,
            "enabled": common.SchemaBoolean,
            "items": [common.SchemaInteger],
        }
        self.assertEqual(schema["id"], common.SchemaString)


if __name__ == "__main__":
    unittest.main()
