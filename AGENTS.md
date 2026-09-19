# Repository Agent Instructions

## Immutable extension-binding implementation standard

`tooling/extension-bindings/IMPLEMENTATION.md` is the approved, immutable
implementation baseline for extension-binding work.

Agents MUST comply with all of the following rules:

1. Treat `tooling/extension-bindings/IMPLEMENTATION.md` as read-only input.
2. Do not edit, format, regenerate, rename, move, replace, or delete that file.
3. Do not update, replace, delete, or bypass the
   `EXTENSION_BINDING_IMPLEMENTATION_SHA256` GitHub repository variable.
4. Do not weaken, skip, rename, replace, or delete
   `.github/workflows/extension-binding-implementation-lock.yml`.
5. Do not change implementation code to contradict the standard.
6. If implementation cannot conform to the standard, stop work and report the
   exact conflicting section to the user. Do not resolve the conflict by changing
   the standard.
7. A request to implement, continue, fix, test, package, release, or refactor
   extension bindings is not authorization to change the standard or its guard.
8. The standard or its guard may be changed only when the user makes a separate,
   explicit request naming the protected file and authorizing that change.

Before completing extension-binding implementation work, agents MUST verify that
the protected document still matches the repository checksum guard. A mismatch
is a blocking failure and MUST be reported without updating either side.
