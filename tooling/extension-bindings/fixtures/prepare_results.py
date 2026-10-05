#!/usr/bin/env python3
"""Materialize reviewed Phase 4 consumer workloads and expected results.

No profiler, binding package, or workload code is imported or executed. The Go
helper uses the native AST/formatter and schema/YAML libraries for fixture
preparation only; it does not extract Conditions or discover installed packages.
"""

from __future__ import annotations

import argparse
import ast
import json
import os
import shutil
import subprocess
import sys
import zipfile
from hashlib import sha256
from pathlib import Path
from typing import Any

import jsonschema
import rfc8785
import yaml

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parent
TOOLING = ROOT.parent
CORE = TOOLING.parents[2] / "spec/schema/runtimeconditions.profile.v0.2.0.schema.yaml"
RESOURCES = (
    "runtimeconditions.bindings.yaml",
    "runtimeconditions.binding-model.yaml",
    "runtimeconditions.extension.yaml",
    "runtimeconditions.binding-release.yaml",
)

# Native source inventory and oracle serialization, never a profiler substitute.
GO_HELPER = r"""
package main
import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "go/ast"
 "go/format"
 "go/parser"
 "go/token"
 "os"
 "strconv"
 "github.com/santhosh-tekuri/jsonschema/v6"
 "gopkg.in/yaml.v3"
)
func check(err error) { if err != nil { panic(err) } }
func main() {
 var request struct {
  Mode, Path, Package, Source, Coordinate string
  Functions []string
  Document, Schema, Condition map[string]any
 }
 check(json.NewDecoder(os.Stdin).Decode(&request))
 switch request.Mode {
 case "yaml":
  data, err := yaml.Marshal(request.Document); check(err); fmt.Print(string(data))
 case "format":
  data, err := format.Source([]byte(request.Source)); check(err); fmt.Print(string(data))
 case "locations":
  fset := token.NewFileSet(); file, err := parser.ParseFile(fset, "<workload>/main.go", request.Source, 0); check(err)
  result := map[string]any{}
  ast.Inspect(file, func(node ast.Node) bool {
   call, ok := node.(*ast.CallExpr); if !ok { return true }
   selector, ok := call.Fun.(*ast.SelectorExpr); if !ok { return true }
   found := false; for _, name := range request.Functions { if name == selector.Sel.Name { found = true } }
   if !found { return true }
   args := []string{}; for _, arg := range call.Args { args = append(args, fset.Position(arg.Pos()).String()) }
   result = map[string]any{"call": fset.Position(call.Pos()).String(), "arguments": args, "function": selector.Sel.Name}
   return false
  })
  if len(result) == 0 { panic("no declaration location found") }; check(json.NewEncoder(os.Stdout).Encode(result))
 case "schema-error":
  hash := sha256.Sum256([]byte(request.Coordinate))
  uri := "urn:runtimeconditions:installed-schema:" + hex.EncodeToString(hash[:])
  data, err := json.Marshal(request.Schema); check(err)
  resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(data)); check(err)
  compiler := jsonschema.NewCompiler(); compiler.DefaultDraft(jsonschema.Draft2020); compiler.AssertFormat()
  check(compiler.AddResource(uri, resource)); schema, err := compiler.Compile(uri); check(err)
  if err := schema.Validate(request.Condition); err != nil {
   fmt.Printf("runtimeconditions: conditions[0] fails extension schema %s: %v\n", request.Coordinate, err)
  } else { panic("negative oracle passed its schema") }
 case "calls":
  fset := token.NewFileSet()
  file, err := parser.ParseFile(fset, request.Path, nil, parser.ParseComments); check(err)
  alias := ""
  for _, item := range file.Imports {
   path, err := strconv.Unquote(item.Path.Value); check(err)
   if path == request.Package && item.Name != nil { alias = item.Name.Name }
  }
  if alias == "" && len(request.Functions) != 0 { panic("conformance source has no explicit package alias") }
  functions := map[string]bool{}; for _, name := range request.Functions { functions[name] = true }
  isCall := func(expr ast.Expr) (*ast.CallExpr, string) {
   call, ok := expr.(*ast.CallExpr); if !ok { return nil, "" }
   selector, ok := call.Fun.(*ast.SelectorExpr); if !ok { return nil, "" }
   receiver, ok := selector.X.(*ast.Ident)
   if !ok || receiver.Name != alias || !functions[selector.Sel.Name] { return nil, "" }
   return call, selector.Sel.Name
  }
  var kept []ast.Decl; var calls []*ast.CallExpr; var names []string
  for _, decl := range file.Decls {
   gen, ok := decl.(*ast.GenDecl)
   if !ok || gen.Tok != token.VAR { kept = append(kept, decl); continue }
   copyGen := *gen; copyGen.Specs = nil
   for _, spec := range gen.Specs {
    value, ok := spec.(*ast.ValueSpec)
    if ok && len(value.Values) == 1 {
     if call, name := isCall(value.Values[0]); call != nil {
      calls = append(calls, call); names = append(names, name); continue
     }
    }
    copyGen.Specs = append(copyGen.Specs, spec)
   }
   if len(copyGen.Specs) != 0 { kept = append(kept, &copyGen) }
  }
  result := struct { Calls []map[string]any `json:"calls"`; Deferred []string `json:"deferred"` }{
   Calls: []map[string]any{}, Deferred: []string{},
  }
  for index, call := range calls {
   copyFile := *file; copyFile.Name = ast.NewIdent("main")
   copyFile.Decls = append(append([]ast.Decl(nil), kept...), &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
    &ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("_")}, Values: []ast.Expr{call}},
   }})
   var output bytes.Buffer; check(format.Node(&output, fset, &copyFile))
   output.WriteString("\nfunc init() { panic(\"workload source must not execute\") }\nfunc main() {}\n")
   formatted, err := format.Source(output.Bytes()); check(err)
   result.Calls = append(result.Calls, map[string]any{"function": names[index], "source": string(formatted)})
  }
  for _, name := range request.Functions {
   found := false; for _, called := range names { if called == name { found = true } }
   if !found { result.Deferred = append(result.Deferred, name) }
  }
  check(json.NewEncoder(os.Stdout).Encode(result))
 default: panic("unknown helper mode")
 }
}
"""


def fail(message: str) -> None:
    raise SystemExit(message)


def read_yaml(path: Path) -> dict[str, Any]:
    value = yaml.safe_load(path.read_bytes())
    if not isinstance(value, dict):
        fail(f"{path}: expected a mapping")
    return value


def write_yaml(path: Path, value: dict[str, Any]) -> None:
    path.write_text(
        yaml.safe_dump(value, sort_keys=False, allow_unicode=True), encoding="utf-8"
    )


def hash_file(path: Path) -> str:
    return sha256(path.read_bytes()).hexdigest()


def helper(binary: Path, **request: Any) -> str:
    return subprocess.run(
        [str(binary)],
        input=json.dumps(request),
        text=True,
        capture_output=True,
        check=True,
    ).stdout


def python_calls(package: Path, manifest: dict[str, Any]) -> dict[str, Any]:
    path = package / "_conformance.py"
    tree = ast.parse(path.read_text(encoding="utf-8"))
    functions = [
        node
        for node in tree.body
        if isinstance(node, ast.FunctionDef) and node.name == "exercise"
    ]
    if len(functions) != 1:
        fail(f"{path}: expected one exercise function")
    setup: list[ast.stmt] = []
    calls: list[tuple[str, ast.stmt]] = []
    deferred: list[str] = []
    for node in functions[0].body:
        target = (
            node.target
            if isinstance(node, ast.AnnAssign)
            else (
                node.targets[0]
                if isinstance(node, ast.Assign) and len(node.targets) == 1
                else None
            )
        )
        name = target.id if isinstance(target, ast.Name) else ""
        value = getattr(node, "value", None)
        if name.startswith(("_declaration_", "_imported_")):
            if not isinstance(value, ast.Call) or not isinstance(
                value.func, ast.Attribute
            ):
                fail(f"{path}: declaration is not a direct package call")
            calls.append((value.func.attr, node))
        elif name.startswith("_deferred_"):
            if not isinstance(value, ast.Attribute):
                fail(f"{path}: deferred declaration is not a function reference")
            deferred.append(value.attr)
        elif not isinstance(node, ast.Pass):
            setup.append(node)
    prefix = f"from __future__ import annotations\nimport {manifest['package']['name']} as b\n"
    return {
        "calls": [
            {
                "function": name,
                "source": prefix
                + "\n".join(ast.unparse(node) for node in (*setup, call))
                + '\nraise RuntimeError("workload source must not execute")\n',
            }
            for name, call in calls
        ],
        "deferred": deferred,
    }


def load_packages(
    fixtures: Path, language: str, binary: Path
) -> dict[str, dict[str, Any]]:
    result = {}
    for tree in sorted((fixtures / language / "packages").iterdir()):
        if not tree.is_dir():
            continue
        package = tree if language == "go" else next((tree / "src").iterdir())
        documents = [read_yaml(package / name) for name in RESOURCES]
        manifest, model, extension, release = documents
        for document, schema_name in zip(
            documents,
            (
                "runtimeconditions.binding-manifest.schema.yaml",
                "runtimeconditions.binding-model.schema.yaml",
                "runtimeconditions.extension-semantic.schema.yaml",
                "runtimeconditions.binding-release.schema.yaml",
            ),
            strict=True,
        ):
            jsonschema.Draft202012Validator(
                read_yaml(TOOLING / "model" / schema_name)
            ).validate(document)
        model_copy = json.loads(json.dumps(model))
        model_digest = model_copy["metadata"].pop("semanticSha256")
        if sha256(rfc8785.dumps(model_copy)).hexdigest() != model_digest:
            fail(f"{tree.name}: model digest differs from its bytes")
        if (
            manifest["model"]["semanticSha256"] != model_digest
            or release["model"]["semanticSha256"] != model_digest
        ):
            fail(f"{tree.name}: manifest/model/release digests disagree")
        if (
            release["package"]["packageKey"] != tree.name
            or manifest["package"]["language"] != language
        ):
            fail(f"{tree.name}: package identity differs")
        for key in ("coordinate", "name", "version"):
            if manifest["package"][key] != release["package"][key]:
                fail(f"{tree.name}: release package.{key} differs")
        if release["provenance"]["mode"] != "test-fixture":
            fail(f"{tree.name}: expected test-only provenance")
        root = model["rootExtension"]
        if (
            release["rootExtension"] != root
            or manifest["extension"]
            != {
                "id": root["id"],
                "semanticSha256": root["semanticSha256"],
            }
            or extension["metadata"]["uri"] + ":" + extension["metadata"]["version"] != root["id"]
        ):
            fail(f"{tree.name}: extension identities disagree")
        locked = next(
            item
            for item in release["dependencyLock"]["extensions"]
            if item["id"] == root["id"]
        )
        if locked["sourceSha256"] != hash_file(package / RESOURCES[2]):
            fail(f"{tree.name}: extension source digest differs")
        identity = manifest["package"]
        archive = (
            fixtures
            / "go/proxy"
            / identity["coordinate"]
            / "@v"
            / (identity["version"] + ".zip")
            if language == "go"
            else fixtures
            / "python/wheels"
            / (
                identity["coordinate"].replace("-", "_")
                + "-"
                + identity["version"]
                + "-py3-none-any.whl"
            )
        )
        with zipfile.ZipFile(archive) as zipped:
            prefix = (
                identity["coordinate"] + "@" + identity["version"] + "/"
                if language == "go"
                else identity["name"] + "/"
            )
            for name in RESOURCES:
                if zipped.read(prefix + name) != (package / name).read_bytes():
                    fail(f"{tree.name}: archived {name} differs")
        generated = (
            json.loads(
                helper(
                    binary,
                    Mode="calls",
                    Path=str(package / "conformance/conformance_test.go"),
                    Package=identity["coordinate"],
                    Functions=[item["function"] for item in manifest["declarations"]],
                )
            )
            if language == "go"
            else python_calls(package, manifest)
        )
        result[tree.name] = {
            "path": package,
            "manifest": manifest,
            "model": model,
            "extension": extension,
            "release": release,
            "archive": archive,
            "generated": generated,
        }
    if not result:
        fail(f"{fixtures}: no assembled {language} packages")
    for item in result.values():
        for dependency in item["release"]["packageDependencies"]:
            matches = [
                candidate
                for candidate in result.values()
                if candidate["release"]["package"]["coordinate"]
                == dependency["coordinate"]
            ]
            if (
                len(matches) != 1
                or hash_file(matches[0]["archive"]) != dependency["artifact"]["sha256"]
            ):
                fail("dependency archive identity or digest differs")
    return result


def source_text(
    spec: dict[str, Any],
    packages: dict[str, dict[str, Any]],
    language: str,
    binary: Path,
) -> str:
    if "generated" in spec:
        generated = spec["generated"]
        entry = packages[generated["package"]]["generated"]["calls"][generated["index"]]
        if entry["function"] != generated["function"]:
            fail("generated declaration function differs from the reviewed inventory")
        return entry["source"]
    source = spec["source"]
    if language == "go":
        return helper(
            binary,
            Mode="format",
            Source=source
            + '\nfunc init() { panic("workload source must not execute") }\nfunc main() {}\n',
        )
    ast.parse(source)
    return source + '\nraise RuntimeError("workload source must not execute")\n'


def applicable_schemas(
    condition: dict[str, Any], closure: set[str], definitions: dict[str, dict[str, Any]]
) -> list[tuple[str, dict[str, Any]]]:
    result = []
    for owner in sorted(closure):
        for schema in definitions[owner]["spec"].get("schemas", []):
            if schema.get("appliesToKind", condition["kind"]) != condition["kind"]:
                continue
            if (
                schema.get("appliesToInterfaceType", condition["interface"]["type"])
                != condition["interface"]["type"]
            ):
                continue
            result.append((owner + "#schema:" + schema["id"], schema["schema"]))
    return result


def profile(case: dict[str, Any], condition: dict[str, Any]) -> dict[str, Any]:
    return {
        "apiVersion": "runtimeconditions.io/v1alpha1",
        "kind": "RuntimeConditionsProfile",
        "metadata": {"name": "phase4-" + case["id"]},
        "workload": {
            "uri": "https://example.test/phase4/" + case["id"],
            "version": "1.0.0",
        },
        "extensions": sorted(case["extensions"]),
        "conditions": [condition],
    }


def materialize(
    catalog: dict[str, Any], fixtures: Path, output: Path, language: str, binary: Path
) -> dict[str, Any]:
    packages = load_packages(fixtures, language, binary)
    definitions = {
        item["extension"]["metadata"]["uri"] + ":" + item["extension"]["metadata"]["version"]: item["extension"]
        for item in packages.values()
    }
    used_calls: set[tuple[str, int]] = set()
    covered_deferred: set[tuple[str, str]] = set()
    records = []
    for outcome in ("positive", "negative"):
        for case in catalog[outcome]:
            if language not in case["languages"]:
                continue
            spec = case["languages"][language]
            required = spec.get("packages", case["packages"])
            if any(key not in packages for key in required):
                fail(f"{case['id']}: an installed package input is missing")
            if "generated" in spec:
                key = (spec["generated"]["package"], spec["generated"]["index"])
                if key in used_calls:
                    fail(f"{case['id']}: generated declaration appears twice")
                used_calls.add(key)
            for deferred in spec.get("coversDeferred", []):
                covered_deferred.add((deferred["package"], deferred["function"]))
            destination = output / language / "workloads" / case["id"]
            destination.mkdir(parents=True)
            source = source_text(spec, packages, language, binary)
            filename = "main.go" if language == "go" else "app.py"
            (destination / filename).write_text(source, encoding="utf-8")
            for deferred in spec.get("coversDeferred", []):
                identity = packages[deferred["package"]]["manifest"]["package"]
                if language == "go":
                    actual = json.loads(
                        helper(
                            binary,
                            Mode="calls",
                            Path=str(destination / filename),
                            Package=identity["coordinate"],
                            Functions=[deferred["function"]],
                        )
                    )["calls"]
                    if not actual:
                        fail(
                            f"{case['id']}: consumer does not call the deferred declaration"
                        )
                else:
                    tree = ast.parse(source)
                    aliases = {
                        alias.asname or alias.name: alias.name
                        for node in ast.walk(tree)
                        if isinstance(node, ast.Import)
                        for alias in node.names
                    }
                    if not any(
                        isinstance(node, ast.Call)
                        and node.args
                        and isinstance(node.func, ast.Attribute)
                        and isinstance(node.func.value, ast.Name)
                        and aliases.get(node.func.value.id) == identity["name"]
                        and node.func.attr == deferred["function"]
                        for node in ast.walk(tree)
                    ):
                        fail(
                            f"{case['id']}: consumer does not complete the deferred declaration"
                        )
            dependencies = [packages[key]["manifest"]["package"] for key in required]
            if language == "go":
                (destination / "go.mod").write_text(
                    "module example.test/phase4/"
                    + case["id"]
                    + "\n\ngo 1.22\n\nrequire (\n"
                    + "".join(
                        "\t" + item["coordinate"] + " " + item["version"] + "\n"
                        for item in dependencies
                    )
                    + ")\n",
                    encoding="utf-8",
                )
                install = ["go", "mod", "download", "all"]
                command = ["<installed-go-profiler>", "generate", "-dir", "."]
                flags = ("-name", "-workload-uri", "-workload-version", "-out")
                env = {
                    "GOWORK": "off",
                    "GOSUMDB": "off",
                    "GOPROXY": (fixtures / "go/proxy").as_uri(),
                }
            else:
                (destination / "requirements.txt").write_text(
                    "".join(
                        item["coordinate"] + "==" + item["version"] + "\n"
                        for item in dependencies
                    ),
                    encoding="utf-8",
                )
                install = [
                    "<workload-python>",
                    "-m",
                    "pip",
                    "install",
                    "--no-index",
                    "--find-links",
                    str(fixtures / "python/wheels"),
                    "-r",
                    "requirements.txt",
                ]
                command = [
                    "<installed-python-profiler>",
                    "profile",
                    "generate",
                    "--project",
                    ".",
                ]
                flags = ("--name", "--workload-uri", "--workload-version", "--out")
                env = {}
            expected_profile = profile(case, spec["condition"])
            core = read_yaml(CORE)
            jsonschema.Draft202012Validator(
                core, format_checker=jsonschema.FormatChecker()
            ).validate(expected_profile)
            closure: set[str] = set()
            pending = list(case["extensions"])
            while pending:
                owner = pending.pop()
                if owner in closure:
                    continue
                if owner not in definitions:
                    fail(f"{case['id']}: unresolved extension {owner}")
                closure.add(owner)
                pending.extend(definitions[owner]["spec"].get("dependencies", []))
            schemas = applicable_schemas(spec["condition"], closure, definitions)
            dependency_schema = case.get("dependencySchema")
            if dependency_schema:
                dependency_owner = dependency_schema.split("#schema:", 1)[0]
                if dependency_owner not in closure - set(case["extensions"]):
                    fail(
                        f"{case['id']}: schema dependency must be omitted from direct contributors"
                    )
                dependency = definitions[dependency_owner]["spec"]
                if any(
                    dependency.get(name)
                    for name in (
                        "kinds",
                        "interfaceTypes",
                        "conditionFields",
                        "interfaceFields",
                        "fieldValues",
                    )
                ):
                    fail(f"{case['id']}: schema-only dependency declares vocabulary")
                if dependency_schema not in {coordinate for coordinate, _ in schemas}:
                    fail(
                        f"{case['id']}: dependency schema does not apply to this declaration"
                    )
            expected_dir = output / language / "expected"
            expected_dir.mkdir(parents=True, exist_ok=True)
            expected_path = expected_dir / (
                case["id"] + (".yaml" if outcome == "positive" else ".error.yaml")
            )
            if outcome == "positive":
                for _, schema in schemas:
                    jsonschema.Draft202012Validator(schema).validate(spec["condition"])
                # Each kind/interface must resolve to one owner in the full
                # closure, and the reviewed direct contributor list includes it.
                for vocabulary, name, target in (
                    ("kinds", spec["condition"]["kind"], None),
                    (
                        "interfaceTypes",
                        spec["condition"]["interface"]["type"],
                        spec["condition"]["kind"],
                    ),
                ):
                    owners = {
                        owner
                        for owner in closure
                        for item in definitions[owner]["spec"].get(vocabulary, [])
                        if item["name"] == name
                        and (target is None or item["targetKind"] == target)
                    }
                    if len(owners) != 1 or not owners <= set(case["extensions"]):
                        fail(
                            f"{case['id']}: vocabulary ownership differs from the expected contributor list"
                        )
                rendered = (
                    helper(binary, Mode="yaml", Document=expected_profile)
                    if language == "go"
                    else yaml.safe_dump(expected_profile, sort_keys=False)
                )
                expected_path.write_text(rendered, encoding="utf-8")
            else:
                diagnostic = dict(case["diagnostic"])
                if dependency_schema:
                    if diagnostic["coordinate"] != dependency_schema:
                        fail(
                            f"{case['id']}: negative must fail the dependency-only schema"
                        )
                    for coordinate, other_schema in schemas:
                        if coordinate != dependency_schema:
                            jsonschema.Draft202012Validator(other_schema).validate(
                                spec["condition"]
                            )
                schema = next(
                    value
                    for coordinate, value in schemas
                    if coordinate == diagnostic["coordinate"]
                )
                errors = sorted(
                    jsonschema.Draft202012Validator(schema).iter_errors(
                        spec["condition"]
                    ),
                    key=lambda error: (
                        list(map(str, error.absolute_path)),
                        error.message,
                    ),
                )
                if not errors or not any(
                    error.validator == diagnostic["keyword"] for error in errors
                ):
                    fail(
                        f"{case['id']}: negative oracle does not fail its specified constraint"
                    )
                diagnostic.update(
                    {
                        "stage": spec.get("stage", "schema"),
                        "exitCode": "nonzero",
                        "profileOutput": "absent",
                    }
                )
                if diagnostic["stage"] == "schema":
                    if language == "go":
                        diagnostic["stderr"] = helper(
                            binary,
                            Mode="schema-error",
                            Schema=schema,
                            Condition=spec["condition"],
                            Coordinate=diagnostic["coordinate"],
                        )
                    else:
                        error = errors[0]
                        calls = [
                            node
                            for node in ast.walk(ast.parse(source))
                            if isinstance(node, ast.Expr)
                            and isinstance(node.value, ast.Call)
                        ]
                        call = calls[0]
                        pointer = (
                            "/" + "/".join(map(str, error.absolute_path))
                            if error.absolute_path
                            else ""
                        )
                        owner, schema_id = diagnostic["coordinate"].split("#schema:", 1)
                        diagnostic["stderr"] = (
                            f"runtimeconditions: <workload>/app.py:{call.lineno}:{call.col_offset + 1}: conditions[0]{pointer}: extension schema {owner}/{schema_id}: {error.message}\n"
                        )
                else:
                    diagnostic["message"] = spec["message"]
                    if language == "go" and diagnostic["stage"] == "structure":
                        manifest = packages[required[0]]["manifest"]
                        declaration = next(
                            item
                            for item in manifest["declarations"]
                            if item["sourceName"] == spec["condition"]["kind"]
                        )
                        locations = json.loads(
                            helper(
                                binary,
                                Mode="locations",
                                Source=source,
                                Functions=[declaration["function"]],
                            )
                        )
                        binding = next(
                            item
                            for item in manifest["rootBindings"]
                            if item["role"] != "interface"
                            and item["path"]
                            == [
                                {
                                    "name": diagnostic["conditionPointer"].removeprefix(
                                        "/"
                                    )
                                }
                            ]
                        )
                        reference = binding["modelRef"]
                        diagnostic["stderr"] = (
                            f"runtimeconditions: {locations['call']}: {declaration['function']} ({declaration['modelRef']['coordinate']}): "
                            f"{locations['arguments'][1]}: {reference['coordinate']} {reference.get('jsonPointer', '')}: {spec['message']}\n"
                        )
                    elif language == "go":
                        diagnostic["stderr"] = f"runtimeconditions: {spec['message']}\n"
                    else:
                        call = next(
                            node
                            for node in ast.walk(ast.parse(source))
                            if isinstance(node, ast.Expr)
                            and isinstance(node.value, ast.Call)
                        )
                        diagnostic["stderr"] = (
                            f"runtimeconditions: <workload>/app.py:{call.lineno}:{call.col_offset + 1}: {spec['message']}\n"
                        )
                write_yaml(expected_path, diagnostic)
            command += [
                flags[0],
                expected_profile["metadata"]["name"],
                flags[1],
                expected_profile["workload"]["uri"],
                flags[2],
                "1.0.0",
                flags[3],
                "profile.yaml",
            ]
            record = {
                "id": case["id"],
                "case": case["case"],
                "outcome": outcome,
                "workload": str(destination.relative_to(output)),
                "sourceSha256": hash_file(destination / filename),
                "expected": str(expected_path.relative_to(output)),
                "expectedSha256": hash_file(expected_path),
                "install": install,
                "profileCommand": command,
                "environment": env,
                "extensionClosure": sorted(closure),
                "directContributors": sorted(case["extensions"]),
            }
            if "generated" in spec:
                record["emittedDeclaration"] = spec["generated"]
            if dependency_schema:
                record["dependencySchema"] = dependency_schema
            if "coversDeferred" in spec:
                record["coversDeferred"] = spec["coversDeferred"]
            records.append(record)
    all_calls = {
        (key, index)
        for key, item in packages.items()
        for index in range(len(item["generated"]["calls"]))
    }
    all_deferred = {
        (key, function)
        for key, item in packages.items()
        for function in item["generated"]["deferred"]
    }
    if used_calls != all_calls or covered_deferred != all_deferred:
        fail(
            f"{language}: declaration coverage differs; missing calls={sorted(all_calls - used_calls)}, extra calls={sorted(used_calls - all_calls)}, missing deferred={sorted(all_deferred - covered_deferred)}, extra deferred={sorted(covered_deferred - all_deferred)}"
        )
    return {
        "positiveDeclarations": sum(item["outcome"] == "positive" for item in records),
        "negativeDeclarations": sum(item["outcome"] == "negative" for item in records),
        "emittedCalls": len(all_calls),
        "completedDeferredDeclarations": len(all_deferred),
        "packages": [
            {
                "packageKey": key,
                "archive": str(item["archive"]),
                "sha256": hash_file(item["archive"]),
                "modelSemanticSha256": item["model"]["metadata"]["semanticSha256"],
                "resources": {
                    name: hash_file(item["path"] / name) for name in RESOURCES
                },
            }
            for key, item in packages.items()
        ],
        "declarations": records,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--fixtures", type=Path, required=True, help="official assemble.py output"
    )
    parser.add_argument(
        "--output",
        type=Path,
        required=True,
        help="new or empty consumer workload directory",
    )
    parser.add_argument("--language", choices=("go", "python", "both"), default="both")
    args = parser.parse_args()
    fixtures, output = args.fixtures.resolve(), args.output.resolve()
    if TOOLING.parents[1] == output or TOOLING.parents[1] in output.parents:
        fail("consumer output must be outside the extensions repository")
    if output == fixtures or fixtures in output.parents or output in fixtures.parents:
        fail("consumer output and assembled fixture trees must be separate")
    if output.exists() and any(output.iterdir()):
        fail("output must be new or empty")
    catalog = read_yaml(ROOT / "conformance.yaml")
    if catalog.get("apiVersion") != "runtimeconditions.io/fixture-results/v1alpha1":
        fail("unsupported fixture inventory version")
    ids = [
        item["id"] for outcome in ("positive", "negative") for item in catalog[outcome]
    ]
    if len(ids) != len(set(ids)) or any(
        not identifier.replace("-", "").isalnum() for identifier in ids
    ):
        fail("fixture IDs must be unique path-safe identifiers")
    output.mkdir(parents=True, exist_ok=True)
    helper_dir = output / "tools"
    helper_dir.mkdir()
    helper_source, binary = helper_dir / "fixture.go", helper_dir / "rc-fixture-source"
    helper_source.write_text(GO_HELPER, encoding="utf-8")
    environment = os.environ.copy()
    environment["GOCACHE"] = str(helper_dir / "go-build-cache")
    subprocess.run(
        ["go", "build", "-trimpath", "-o", str(binary), str(helper_source)],
        cwd=TOOLING / "emitters/go",
        env=environment,
        check=True,
    )
    inventory = {
        "apiVersion": catalog["apiVersion"],
        "kind": "RuntimeConditionsFixtureResults",
        "status": "prepared-awaiting-installed-cli-acceptance",
        "catalogSha256": hash_file(ROOT / "conformance.yaml"),
        "preparerSha256": hash_file(Path(__file__)),
        "coreSchemaSha256": hash_file(CORE),
        "languages": {},
    }
    for language in ("go", "python") if args.language == "both" else (args.language,):
        inventory["languages"][language] = materialize(
            catalog, fixtures, output, language, binary
        )
    normalizer = output / "normalizer"
    normalizer.mkdir()
    normalizer_binary = normalizer / "rc-binding-model"
    shutil.copy2(fixtures / "tools/rc-binding-model", normalizer_binary)
    for name in (
        "runtimeconditions.extension-semantic.schema.yaml",
        "runtimeconditions.binding-model.schema.yaml",
    ):
        shutil.copyfile(TOOLING / "model" / name, normalizer / name)
    core = read_yaml(CORE)
    normalizer_cases = []
    for case in catalog["normalizerNegatives"]:
        inputs = normalizer / case
        shutil.copytree(TOOLING / "model/conformance/cases" / case, inputs)
        expected = normalizer / (case + ".diagnostic.yaml")
        shutil.copyfile(
            TOOLING / "model/conformance/expected" / case / "diagnostic.yaml", expected
        )
        diagnostic = read_yaml(expected)
        location = " ".join(
            part
            for part in (
                diagnostic.get("coordinate", ""),
                diagnostic.get("jsonPointer", ""),
            )
            if part
        )
        stderr = (
            diagnostic["category"]
            + " "
            + diagnostic["code"]
            + (" at " + location if location else "")
            + ": "
            + diagnostic["message"]
            + "\n"
        )
        normalizer_cases.append(
            {
                "case": case,
                "inputs": str(inputs.relative_to(output)),
                "expected": str(expected.relative_to(output)),
                "expectedSha256": hash_file(expected),
                "exitCode": "nonzero",
                "modelOutput": "absent",
                "stderr": stderr,
                "command": [
                    str(normalizer_binary),
                    "--root",
                    read_yaml(inputs / "root.yaml")["metadata"]["uri"] + ":" + read_yaml(inputs / "root.yaml")["metadata"]["version"],
                    "--extension-root",
                    str(inputs),
                    "--semantic-schema",
                    str(
                        normalizer / "runtimeconditions.extension-semantic.schema.yaml"
                    ),
                    "--model-schema",
                    str(normalizer / "runtimeconditions.binding-model.schema.yaml"),
                    "--core-profile-id",
                    core["$id"],
                    "--core-profile-version",
                    core["x-runtimeconditions-version"],
                    "--core-profile-semantic-sha256",
                    sha256(rfc8785.dumps(core)).hexdigest(),
                    "--normalizer-sha256",
                    hash_file(normalizer_binary),
                    "--output",
                    str(normalizer / (case + ".model.yaml")),
                ],
            }
        )
    inventory["normalizerNegatives"] = normalizer_cases
    write_yaml(output / "inventory.yaml", inventory)
    for language, item in inventory["languages"].items():
        print(
            f"{language}: {item['positiveDeclarations']} positive and {item['negativeDeclarations']} negative workloads; {item['emittedCalls']} emitted calls and {item['completedDeferredDeclarations']} deferred declarations covered"
        )
    print(f"prepared results: {output}")


if __name__ == "__main__":
    main()
