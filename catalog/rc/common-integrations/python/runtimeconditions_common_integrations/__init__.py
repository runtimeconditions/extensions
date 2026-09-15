from __future__ import annotations

from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class Declaration:
    name: str
    options: tuple[Any, ...]


@dataclass(frozen=True)
class Option:
    name: str
    args: tuple[Any, ...]


class Api:
    @staticmethod
    def declare(name: str, *options: Any) -> Declaration:
        return Declaration(name, options)

    @staticmethod
    def spec(format: str, uri: str, version: str = "") -> Option:
        return Option("spec", (format, uri, version))


class Http:
    @staticmethod
    def get(path: str, *options: Any) -> Option:
        return Option("get", (path, *options))

    @staticmethod
    def head(path: str, *options: Any) -> Option:
        return Option("head", (path, *options))

    @staticmethod
    def post(path: str, *options: Any) -> Option:
        return Option("post", (path, *options))

    @staticmethod
    def put(path: str, *options: Any) -> Option:
        return Option("put", (path, *options))

    @staticmethod
    def patch(path: str, *options: Any) -> Option:
        return Option("patch", (path, *options))

    @staticmethod
    def delete(path: str, *options: Any) -> Option:
        return Option("delete", (path, *options))

    @staticmethod
    def options(path: str, *options: Any) -> Option:
        return Option("options", (path, *options))

    @staticmethod
    def trace(path: str, *options: Any) -> Option:
        return Option("trace", (path, *options))

    @staticmethod
    def request(schema: type) -> Option:
        return Option("request", (schema,))

    @staticmethod
    def response(schema: type) -> Option:
        return Option("response", (schema,))


class Datastore:
    class Engine:
        POSTGRES = "postgres"
        MYSQL = "mysql"
        MARIADB = "mariadb"
        SQLSERVER = "sqlserver"
        ORACLE = "oracle"
        SQLITE = "sqlite"
        MONGODB = "mongodb"
        COUCHBASE = "couchbase"

    @staticmethod
    def declare(name: str, *options: Any) -> Declaration:
        return Declaration(name, options)

    @staticmethod
    def relational(engine: str) -> Option:
        return Option("relational", (engine,))

    @staticmethod
    def document(engine: str) -> Option:
        return Option("document", (engine,))


class Cache:
    class Engine:
        REDIS = "redis"
        MEMCACHED = "memcached"

    @staticmethod
    def declare(name: str, *options: Any) -> Declaration:
        return Declaration(name, options)

    @staticmethod
    def key_value(engine: str) -> Option:
        return Option("key_value", (engine,))

