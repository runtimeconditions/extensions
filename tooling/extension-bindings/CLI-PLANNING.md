# Common `rc` CLI planning

This document records a proposed direction for a common Runtime Conditions
command-line entry point. It is planning material, not an amendment to
`IMPLEMENTATION.md` and not authorization to change that protected
implementation standard. The proposal must be reconciled with the standard in a
separate, explicit change before implementation work that requires a different
contract begins.

## Goals

- Give users one recognizable `rc` command for binding generation and profiling.
- Keep the shared binding resolver, normalizer, and orchestrator in Go.
- Keep source analysis in language-specific profilers that use native parsers
  and package metadata APIs.
- Allow every profiler to own its language-specific flags and runtime checks.
- Make language support optional so Go-only users do not need to install Python,
  Java, Rust, Node.js, or Ruby runtimes without using those profilers.
- Distribute the CLI and optional profiler add-ons through a small number of
  operating-system package-manager channels.
- Keep generated language SDKs in their native package ecosystems.
- Preserve exact tool identity and reproducibility for binding verification and
  release workflows.

## Proposed architecture

`rc` is a Go executable built with Cobra. It owns the command tree, shared
configuration and diagnostics, binding orchestration, add-on discovery, and
process routing. It does not parse Python, Java, Rust, JavaScript/TypeScript, or
Ruby source itself. Each profiler remains a separately implemented and released
program in the language it analyzes.

The boundary is a subprocess contract rather than an in-process language plugin
ABI. The core discovers a language profiler executable, passes arguments as an
argument vector without shell evaluation, preserves the working directory and
environment, streams stdout and stderr, propagates cancellation, and returns the
profiler's exit status. Profiles and other machine-readable results can
therefore continue to use stdout without launcher messages corrupting them.

Each add-on should expose a small machine-readable identity/capabilities command
containing at least:

- add-on name and version;
- CLI protocol version;
- supported `rc` and contract/schema version ranges;
- language and minimum runtime/tool requirements;
- the executable command used for profiling.

`rc` can use this metadata for `plugins list`, compatibility checks, and
`doctor` diagnostics. The metadata contract should remain small and should not
attempt to describe every language-specific flag. Language commands own those
flags and their help text.

For production binding builds, `toolchain.lock.yaml` remains authoritative for
the exact normalizer, orchestrator, emitters, profilers, and native toolchain
versions and artifact digests. A release build must execute the locked artifacts
and record their identities; it must not silently use whichever add-on happens
to appear first on `PATH`. Interactive discovery may use the system-installed
add-ons, subject to compatibility checks.

## Proposed command tree

The existing Phase 5 binding command group remains the primary native command
tree:

```text
rc bindings resolve
rc bindings normalize
rc bindings generate
rc bindings verify
rc bindings package
rc bindings plan-release
rc bindings check
rc bindings update
```

The existing `--packages`, `--toolchain-lock`, `--cache`, `--work-dir`, target,
resolution, and output semantics belong to the `bindings` command group and its
relevant child commands. They should not become root flags or profiler flags.

Profiling is routed by language. The language name selects an installed profiler;
the remaining command and arguments are owned by that profiler. Examples:

```text
rc profile go generate --dir ./service --out profile.yaml
rc profile python profile generate --project ./service --python ./venv/bin/python
rc profile java generate --project ./service --java-home /opt/jdk-21 --javac /opt/jdk-21/bin/javac
rc profile rust --help
```

The exact verb shape should be reconciled across profiler CLIs during protocol
design. The important requirement is that the dispatcher does not normalize
language-specific flags such as `--java-home`, `--javac`, `--python`,
`--cargo-home`, `--node`, or `--ruby`. Each frontend validates and documents its
own environment options.

Useful core commands include:

```text
rc --help
rc version
rc doctor
rc plugins list
rc profile --help
```

Cobra owns help, command discovery, argument validation for native `rc`
commands, and shell completion for the static tree. A language profiler's own
help is delegated to that profiler. Dynamic completion of add-on-specific flags
can be considered later; it must not be a reason to couple all profiler flags
to the core binary.

Only flags with genuinely identical meaning across all relevant commands should
be global persistent flags. Candidates include verbosity, color, and a shared
configuration-file location. Binding paths and language runtime paths remain
scoped to their respective command/backend.

## Cobra implementation shape

Use a standard Cobra root command and small command constructors, keeping
business logic in the existing orchestrator and plugin-routing packages:

```text
orchestrator/
  cmd/rc/main.go
  internal/cli/
    root.go
    bindings.go
    profile.go
    plugins.go
    doctor.go
  internal/plugins/
    discover.go
    invoke.go
```

`main.go` should only create and execute the root command, report errors, and
choose the process exit code. `bindings.go` binds Cobra flags to the Phase 5
orchestrator options. `profile.go` routes the language selector and forwards
the remaining arguments untouched. `plugins.go` implements add-on listing and
metadata parsing. `doctor.go` performs read-only environment checks.

For profiler forwarding, the language dispatch command must avoid Cobra
consuming backend flags. Cobra's `DisableFlagParsing` behavior can support this
proxy boundary. The implementation should explicitly define how `--help`,
`--version`, shell completion requests, unknown profiler names, and the `--`
separator are handled so users can predict which process receives each token.
The subprocess invocation must use `exec.CommandContext` or equivalent with an
argument slice, never a shell-assembled command string.

The current `orchestrator/cli.go` manually routes commands and uses Go's standard
`flag` package. A Cobra migration should preserve its behavior and output
contracts rather than changing binding-generation semantics as a side effect.

## Add-on distribution

Publish one system-package channel per supported operating-system family, with
the core and language profilers as packages in that same channel. For example:

| Package | Contents and dependencies |
| --- | --- |
| `rc` | Go CLI, binding orchestrator, Go emitter/tools that are part of the supported core workflow |
| `rc-profiler-go` | Go profiler executable and its compatibility metadata |
| `rc-profiler-python` | Python profiler and dependencies; declares or documents Python 3.11+ |
| `rc-profiler-java` | Java profiler and dependencies; declares or documents required Java tooling |
| `rc-profiler-rust` | Rust profiler and dependencies, when available |
| `rc-profiler-node` | JavaScript/TypeScript profiler and dependencies, when available |
| `rc-profiler-ruby` | Ruby profiler and dependencies, when available |

On macOS, a Homebrew tap can hold a core formula and optional profiler formulae.
On Debian/Ubuntu, a signed APT repository can hold corresponding packages. RPM
repositories can use the same package split if added to the support matrix. The
core install should not pull optional language runtimes. Users install only the
backends they need, through the same operating-system package manager they used
for `rc`. Convenience meta-packages can be offered for users who deliberately
want several or all backends.

Runtime dependencies require a declared support matrix. A system package can
depend on an available runtime version, or the profiler package can provide a
managed runtime if that is an explicit product choice. For example, Python 3.11+
availability varies across supported Linux distributions; the package policy
must not assume that every system repository provides the required minor
version. Java target JDK selection can remain an explicit profiler option even
when the profiler itself has a separate runtime requirement.

Generated binding packages are not installed through these system packages.
They remain dependencies of workload projects and use the native ecosystem:
Go modules, Python package indexes, Maven, npm, Cargo, RubyGems, and others as
applicable. The `rc` system-package channel distributes the tooling; it does not
replace project-level package resolution.

## Compatibility, release, and security requirements

- Version the CLI-to-add-on protocol independently and reject unsupported
  protocol versions with an actionable message.
- Define supported `rc`/profiler compatibility ranges and contract/schema
  versions in add-on metadata.
- Keep locked release verification separate from ambient plugin discovery.
- Record exact executable artifact digests and native tool versions in release
  provenance as already required by the implementation standard.
- Invoke only trusted, resolved executable paths and never evaluate plugin
  metadata as code.
- Treat profiler output as data; preserve stdout exactly and prefix only
  human-facing launcher diagnostics on stderr.
- Ensure package-manager dependencies are optional by language, with a clear
  install and removal path for each add-on.
- Keep the add-on protocol backward-compatible where practical; make breaking
  protocol changes explicit and versioned.

## Suggested rollout

1. Review this proposal against the protected implementation standard and
   explicitly authorize any required change before altering that standard.
2. Adopt Cobra for the existing Go `rc` entry point while preserving Phase 5
   binding command behavior.
3. Define and document the profiler add-on metadata and subprocess contract.
4. Integrate the existing Go and Python profiler CLIs behind `rc profile` and
   verify that their language-specific flags, output, and exit statuses pass
   through correctly.
5. Add `plugins list` and `doctor` runtime/compatibility reporting.
6. Package the core and existing profiler add-ons through the selected Homebrew
   and Linux repository channels.
7. Add Java, Rust, JS/TS, Ruby, and later profilers as independently released
   add-ons without changing the core command contract unless a shared capability
   is added.

## Decisions still needed

- Whether profiler invocations use one standardized operation vocabulary or
  expose each backend's full native subcommand tree after the language selector.
- The exact plugin executable naming and lookup order, including install-relative
  paths versus `PATH`.
- Which global `rc` flags, if any, have uniform semantics.
- Whether each OS package manager installs only available runtime dependencies
  or can offer managed runtime packages for unsupported system versions.
- Initial OS, architecture, and Linux distribution support.
- Whether dynamic shell completion for profiler-owned flags is a launch
  requirement or a later enhancement.
