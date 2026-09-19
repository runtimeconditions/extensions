from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum
from typing import ClassVar, Sequence


class Property(StrEnum):
    URL = "url"
    BASE_URL = "baseUrl"
    HOSTNAME = "hostname"
    PORT = "port"
    SCHEME = "scheme"
    USERNAME = "username"
    PASSWORD = "password"
    DATABASE = "database"
    TOKEN = "token"
    TLS = "tls"


@dataclass(frozen=True, kw_only=True)
class Env:
    property: Property
    name: str
    sensitive: bool | None = None
    required: bool | None = None


@dataclass(frozen=True, kw_only=True)
class AlternativesItem:
    env: Sequence[Env]


type Alternatives = Sequence[AlternativesItem]


@dataclass(frozen=True, kw_only=True)
class Configuration:
    runtimeconditions_api_field: ClassVar[bool] = True
    runtimeconditions_datastore_field: ClassVar[bool] = True
    runtimeconditions_cache_field: ClassVar[bool] = True

    env: Sequence[Env] | None = None
    alternatives: Alternatives | None = None
