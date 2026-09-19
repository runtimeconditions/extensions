from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum
from typing import ClassVar, Protocol, Sequence


@dataclass(frozen=True)
class Declaration:
    pass


class APIField(Protocol):
    runtimeconditions_api_field: ClassVar[bool]


class DatastoreField(Protocol):
    runtimeconditions_datastore_field: ClassVar[bool]


class CacheField(Protocol):
    runtimeconditions_cache_field: ClassVar[bool]


def api(*fields: APIField) -> Declaration:
    return Declaration()


def datastore(*fields: DatastoreField) -> Declaration:
    return Declaration()


def cache(*fields: CacheField) -> Declaration:
    return Declaration()


class HTTPMethod(StrEnum):
    GET = "GET"
    HEAD = "HEAD"
    POST = "POST"
    PUT = "PUT"
    PATCH = "PATCH"
    DELETE = "DELETE"
    OPTIONS = "OPTIONS"
    TRACE = "TRACE"


class SchemaScalar(StrEnum):
    STRING = "string"
    NUMBER = "number"
    INTEGER = "integer"
    BOOLEAN = "boolean"
    NULL = "null"


type SimpleSchema = SchemaScalar | dict[str, SimpleSchema] | Sequence[SimpleSchema]

SchemaString = SchemaScalar.STRING
SchemaNumber = SchemaScalar.NUMBER
SchemaInteger = SchemaScalar.INTEGER
SchemaBoolean = SchemaScalar.BOOLEAN
SchemaNull = SchemaScalar.NULL


@dataclass(frozen=True, kw_only=True)
class Spec:
    uri: str
    version: str | None = None


@dataclass(frozen=True, kw_only=True)
class OperationsItem:
    method: HTTPMethod
    path: str
    request_body_schema: SimpleSchema | None = None
    response_schema: SimpleSchema | None = None


type Operations = Sequence[OperationsItem]


@dataclass(frozen=True, kw_only=True)
class Http:
    runtimeconditions_api_field: ClassVar[bool] = True

    spec: Spec | None = None
    operations: Operations = ()


class RelationalEngine(StrEnum):
    POSTGRES = "postgres"
    MYSQL = "mysql"
    MARIADB = "mariadb"
    SQLSERVER = "sqlserver"
    ORACLE = "oracle"
    SQLITE = "sqlite"


@dataclass(frozen=True, kw_only=True)
class Relational:
    runtimeconditions_datastore_field: ClassVar[bool] = True

    engine: RelationalEngine | None = None


class DocumentEngine(StrEnum):
    MONGODB = "mongodb"
    COUCHBASE = "couchbase"


@dataclass(frozen=True, kw_only=True)
class Document:
    runtimeconditions_datastore_field: ClassVar[bool] = True

    engine: DocumentEngine | None = None


class KeyValueEngine(StrEnum):
    REDIS = "redis"
    MEMCACHED = "memcached"


@dataclass(frozen=True, kw_only=True)
class KeyValue:
    runtimeconditions_cache_field: ClassVar[bool] = True

    engine: KeyValueEngine | None = None
