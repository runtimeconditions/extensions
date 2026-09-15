from __future__ import annotations

from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class EnvInput:
    property: str
    name: str
    options: tuple[Any, ...]


@dataclass(frozen=True)
class EnvOption:
    name: str


@dataclass(frozen=True)
class EnvAlternative:
    env: tuple[EnvInput, ...]


class EnvConfiguration:
    @staticmethod
    def env(property: str, name: str, *options: Any) -> EnvInput:
        return EnvInput(property, name, options)

    @staticmethod
    def env_alternative(*env: EnvInput) -> EnvAlternative:
        return EnvAlternative(env)

    @staticmethod
    def sensitive() -> EnvOption:
        return EnvOption("sensitive")

    @staticmethod
    def optional() -> EnvOption:
        return EnvOption("optional")

