// Package commonintegrations is a structural binding-model fixture generated
// from the Common Integrations extension definition.
package commonintegrations

// Declaration is the inert result of a Condition declaration.
type Declaration struct{}

// APIField is a root field accepted by an api Condition.
type APIField interface {
	RuntimeConditionsAPIField()
}

// DatastoreField is a root field accepted by a datastore Condition.
type DatastoreField interface {
	RuntimeConditionsDatastoreField()
}

// CacheField is a root field accepted by a cache Condition.
type CacheField interface {
	RuntimeConditionsCacheField()
}

// API declares an api Condition from flat root fields.
func API(fields ...APIField) Declaration { return Declaration{} }

// Datastore declares a datastore Condition from flat root fields.
func Datastore(fields ...DatastoreField) Declaration { return Declaration{} }

// Cache declares a cache Condition from flat root fields.
func Cache(fields ...CacheField) Declaration { return Declaration{} }

// HTTPMethod is the enum at interface.operations[].method in the http scope.
type HTTPMethod string

const (
	GET     HTTPMethod = "GET"
	HEAD    HTTPMethod = "HEAD"
	POST    HTTPMethod = "POST"
	PUT     HTTPMethod = "PUT"
	PATCH   HTTPMethod = "PATCH"
	DELETE  HTTPMethod = "DELETE"
	OPTIONS HTTPMethod = "OPTIONS"
	TRACE   HTTPMethod = "TRACE"
)

// SimpleSchema is the recursive #/$defs/simpleSchema union.
type SimpleSchema interface {
	runtimeConditionsSimpleSchema()
}

type scalarSchema string

func (scalarSchema) runtimeConditionsSimpleSchema() {}

const (
	SchemaString  scalarSchema = "string"
	SchemaNumber  scalarSchema = "number"
	SchemaInteger scalarSchema = "integer"
	SchemaBoolean scalarSchema = "boolean"
	SchemaNull    scalarSchema = "null"
)

// SchemaObject is the object branch of SimpleSchema.
type SchemaObject map[string]SimpleSchema

func (SchemaObject) runtimeConditionsSimpleSchema() {}

// SchemaArray is the array branch of SimpleSchema. The extension schema
// requires exactly one item.
type SchemaArray []SimpleSchema

func (SchemaArray) runtimeConditionsSimpleSchema() {}

// Spec is the object at interface.spec. Its format field is the fixed value
// "openapi" and therefore is not caller supplied.
type Spec struct {
	URI     string
	Version string
}

// Operations is the array at interface.operations.
type Operations []OperationsItem

// OperationsItem is the object at interface.operations[].
type OperationsItem struct {
	Method            HTTPMethod
	Path              string
	RequestBodySchema SimpleSchema
	ResponseSchema    SimpleSchema
}

// Http is the interface object whose fixed type is "http".
type Http struct {
	Spec       *Spec
	Operations Operations
}

func (Http) RuntimeConditionsAPIField() {}

// RelationalEngine is the enum at interface.engine in the relational scope.
type RelationalEngine string

const (
	Postgres  RelationalEngine = "postgres"
	MySQL     RelationalEngine = "mysql"
	MariaDB   RelationalEngine = "mariadb"
	SQLServer RelationalEngine = "sqlserver"
	Oracle    RelationalEngine = "oracle"
	SQLite    RelationalEngine = "sqlite"
)

// Relational is the interface object whose fixed type is "relational".
type Relational struct {
	Engine RelationalEngine
}

func (Relational) RuntimeConditionsDatastoreField() {}

// DocumentEngine is the enum at interface.engine in the document scope.
type DocumentEngine string

const (
	MongoDB   DocumentEngine = "mongodb"
	Couchbase DocumentEngine = "couchbase"
)

// Document is the interface object whose fixed type is "document".
type Document struct {
	Engine DocumentEngine
}

func (Document) RuntimeConditionsDatastoreField() {}

// KeyValueEngine is the enum at interface.engine in the key_value scope.
type KeyValueEngine string

const (
	Redis     KeyValueEngine = "redis"
	Memcached KeyValueEngine = "memcached"
)

// KeyValue is the interface object whose fixed type is "key_value".
type KeyValue struct {
	Engine KeyValueEngine
}

func (KeyValue) RuntimeConditionsCacheField() {}
