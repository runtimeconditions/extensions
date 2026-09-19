package structuralmodel_test

import (
	"testing"

	common "github.com/runtimeconditions/extensions/tooling/extension-bindings/structural-model/go/commonintegrations"
	env "github.com/runtimeconditions/extensions/tooling/extension-bindings/structural-model/go/envconfiguration"
)

func TestCommonAndEnvironmentConfigurationShapesCompose(t *testing.T) {
	_ = common.API(
		common.Http{
			Spec: &common.Spec{URI: "https://example.test/openapi.yaml"},
			Operations: common.Operations{
				{
					Method: common.GET,
					Path:   "/todos",
					ResponseSchema: common.SchemaObject{
						"id": common.SchemaString,
					},
				},
			},
		},
		env.Configuration{
			Env: []env.Env{
				{Property: env.BaseURL, Name: "TODOS_API_URL"},
				{Property: env.Token, Name: "TODOS_API_TOKEN"},
			},
		},
	)

	_ = common.Datastore(
		common.Relational{Engine: common.Postgres},
		env.Configuration{
			Env: []env.Env{
				{Property: env.Database, Name: "DATABASE_NAME"},
			},
		},
	)

	_ = common.Cache(
		common.KeyValue{Engine: common.Redis},
		env.Configuration{
			Alternatives: env.Alternatives{
				{
					Env: []env.Env{
						{Property: env.URL, Name: "REDIS_URL"},
					},
				},
				{
					Env: []env.Env{
						{Property: env.Hostname, Name: "REDIS_HOST"},
						{Property: env.Port, Name: "REDIS_PORT"},
					},
				},
			},
		},
	)
}

func TestSchemaVariantsAreNativeValues(t *testing.T) {
	var schema common.SimpleSchema = common.SchemaObject{
		"id":      common.SchemaString,
		"enabled": common.SchemaBoolean,
		"items": common.SchemaArray{
			common.SchemaInteger,
		},
	}
	if schema == nil {
		t.Fatal("schema should be represented by its native structural value")
	}
}
