use serde_json::Value;
use std::collections::{BTreeMap, HashMap};
use std::path::{Path, PathBuf};
use std::process::{Command, Output};
use std::{env, fs};

fn bin(args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_runtimeconditions-binding-emitter-rust")).args(args).output().unwrap()
}

fn yaml(p: &Path) -> Value {
    serde_yaml::from_str(&fs::read_to_string(p).unwrap()).unwrap()
}

fn root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../model")
}

fn scratch(name: &str) -> PathBuf {
    let p = env::temp_dir().join(format!("rc-rust-emitter-{}-{name}", std::process::id()));
    let _ = fs::remove_dir_all(&p);
    p
}

fn target(model: &Value, key: &str, dir: &Path) -> PathBuf {
    let id = model["rootExtension"]["id"].as_str().unwrap();
    let ext = model["extensions"].as_array().unwrap().iter().find(|x| x["id"] == id).unwrap();
    let mut deps: Vec<&str> = ext["dependencies"].as_array().map_or(vec![], |d| d.iter().filter_map(Value::as_str).collect());
    deps.sort();
    let deps: Vec<Value> = deps.iter().enumerate().map(|(i, d)| serde_json::json!({"extension": d, "crateName": format!("rc-dep-{i}"), "version": "1.0.0"})).collect();
    let t = serde_json::json!({
        "apiVersion": "runtimeconditions.io/rust-package-target/v1alpha1", "kind": "RuntimeConditionsRustPackageTarget",
        "packageKey": key, "rootExtension": id, "crateName": format!("rc-{key}"), "version": "1.0.0",
        "sourceDirectory": format!("bindings/{key}/rust"), "minimumRustVersion": "1.75", "publicationMode": "github-tag",
        "repositoryUrl": "https://github.com/runtimeconditions/extensions", "emitterSha256": "0".repeat(64), "dependencies": deps,
    });
    let p = dir.join("target.yaml");
    fs::write(&p, serde_yaml::to_string(&t).unwrap()).unwrap();
    p
}

fn models() -> Vec<(String, PathBuf)> {
    let mut v: Vec<_> = fs::read_dir(root().join("conformance/expected"))
        .unwrap()
        .map(|d| d.unwrap().path())
        .filter(|p| p.join("runtimeconditions.binding-model.yaml").exists())
        .map(|p| (p.file_name().unwrap().to_string_lossy().into_owned(), p.join("runtimeconditions.binding-model.yaml")))
        .collect();
    v.sort();
    v
}

fn val(r: &Value, types: &HashMap<&str, &Value>, stack: &mut Vec<String>) -> Option<String> {
    if let Some(b) = r["builtin"].as_str() {
        return Some(match b {
            "String" => "String::new()",
            "bool" => "false",
            "i64" => "0",
            "f64" => "0.0",
            "()" => "()",
            _ => "serde_json::Value::Null",
        }.into());
    }
    ctor(r["type"].as_str().unwrap(), types, stack)
}

fn named(r: &Value, expr: String) -> String {
    if r.get("type").is_some() { format!("{expr}.into()") } else { expr }
}

fn ctor(name: &str, types: &HashMap<&str, &Value>, stack: &mut Vec<String>) -> Option<String> {
    if stack.iter().any(|s| s == name) {
        return None;
    }
    stack.push(name.into());
    let t = types[name];
    let out = match t["construct"].as_str().unwrap() {
        "object" | "wrapper" => {
            let mut fields = vec![];
            for f in t["fields"].as_array().unwrap() {
                let v = val(&f["value"], types, stack).map(|e| named(&f["value"], e));
                let fnm = f["nativeName"].as_str().unwrap();
                fields.push(match (f["required"].as_bool().unwrap(), v) {
                    (true, Some(e)) => format!("{fnm}: {e}"),
                    (true, None) => panic!("{name}.{fnm}: required cycle"),
                    (false, Some(e)) => format!("{fnm}: Some({e})"),
                    (false, None) => format!("{fnm}: None"),
                });
            }
            Some(format!("{name} {{ {} }}", fields.join(", ")))
        }
        "scalar" => Some(format!("{name}::{}", t["members"][0]["nativeName"].as_str().unwrap())),
        "collection" => Some(format!("{name}(vec![{}])", val(&t["element"]["value"], types, stack).unwrap_or_default())),
        "map" => Some(match val(&t["element"]["value"], types, stack) {
            Some(e) => format!("{name}([(\"k\".to_string(), {e})].into_iter().collect())"),
            None => format!("{name}(Default::default())"),
        }),
        _ => (0..t["variants"].as_array().unwrap().len()).find_map(|i| variant(name, t, i, types, stack)),
    };
    stack.pop();
    out
}

fn variant(name: &str, t: &Value, i: usize, types: &HashMap<&str, &Value>, stack: &mut Vec<String>) -> Option<String> {
    let v = &t["variants"][i]["value"];
    val(v, types, stack).map(|e| format!("{name}::Variant{}({})", i + 1, named(v, e)))
}

fn exercise(manifest: &Value, krate: &str, round_trip: bool) -> String {
    let all = manifest["types"].as_array().unwrap();
    let types: HashMap<&str, &Value> = all.iter().map(|t| (t["nativeName"].as_str().unwrap(), t)).collect();
    let mut body = String::new();
    for t in all {
        let name = t["nativeName"].as_str().unwrap();
        let built = ctor(name, &types, &mut vec![]).unwrap_or_else(|| panic!("{name}: cannot be constructed"));
        body += &format!("    let _ = {built};\n");
        if round_trip {
            body += &format!("    {{ let v = {built}; let j = serde_json::to_string(&v).unwrap(); let back: {name} = serde_json::from_str(&j).unwrap(); assert_eq!(v, back, \"{name}\"); }}\n");
        }
        for m in t["members"].as_array().into_iter().flatten() {
            body += &format!("    assert_eq!({name}::{}.value(), {:?});\n", m["nativeName"].as_str().unwrap(), m["value"].as_str().unwrap());
        }
        for i in 0..t["variants"].as_array().map_or(0, Vec::len) {
            if let Some(v) = variant(name, t, i, &types, &mut vec![name.to_string()]) {
                body += &format!("    let _ = {v};\n");
            }
        }
    }
    for d in manifest["declarations"].as_array().unwrap() {
        let objs: Vec<String> = all
            .iter()
            .filter(|t| t["implements"].as_array().is_some_and(|i| i.iter().any(|x| x["markerMethod"] == d["markerMethod"])))
            .map(|t| format!("&{}", ctor(t["nativeName"].as_str().unwrap(), &types, &mut vec![]).unwrap()))
            .collect();
        body += &format!("    let _ = {}(&[{}]);\n", d["function"].as_str().unwrap(), objs.join(", "));
    }
    format!("#![allow(dead_code, unused)]\nuse {krate}::*;\nfn main() {{\n{body}}}\n")
}

fn rustc(args: &[&str]) -> Output {
    Command::new("rustc").args(["--edition", "2021"]).args(args).output().unwrap()
}

fn compile_and_run(key: &str, dir: &Path, out: &Path, manifest: &Value, extra: &[String]) {
    let mut stubs: BTreeMap<String, String> = BTreeMap::new();
    for c in manifest["importedMarkerContracts"].as_array().unwrap() {
        let krate = c["providerPackage"].as_str().unwrap().replace('-', "_");
        *stubs.entry(krate).or_default() += &format!("pub trait {} {{ fn {}(&self); }}\n", c["markerInterface"].as_str().unwrap(), c["markerMethod"].as_str().unwrap());
    }
    let build = |suffix: &str| -> Output {
        let lib = dir.join(format!("stub{suffix}"));
        fs::create_dir_all(&lib).unwrap();
        let mut ext = vec![];
        for (k, src) in &stubs {
            let (f, o) = (lib.join(format!("{k}.rs")), lib.join(format!("lib{k}.rlib")));
            fs::write(&f, if suffix.is_empty() { src.clone() } else { src.replace("(&self)", "_x(&self)") }).unwrap();
            let r = rustc(&["--crate-type", "rlib", "--crate-name", k, f.to_str().unwrap(), "-o", o.to_str().unwrap()]);
            assert!(r.status.success(), "{key}: stub {k}: {}", String::from_utf8_lossy(&r.stderr));
            ext.push(format!("{k}={}", o.display()));
        }
        let package = lib.join("libgenpkg.rlib");
        let mut lib_args: Vec<String> = ["--crate-type", "rlib", "--crate-name", "genpkg", "-D", "warnings"].map(String::from).to_vec();
        lib_args.extend([out.join("src/lib.rs").to_str().unwrap().into(), "-o".into(), package.to_str().unwrap().into()]);
        lib_args.extend(extra.iter().cloned());
        ext.iter().for_each(|e| lib_args.extend(["--extern".into(), e.clone()]));
        let built = rustc(&lib_args.iter().map(String::as_str).collect::<Vec<_>>());
        if !built.status.success() {
            return built;
        }
        let main = lib.join("main.rs");
        fs::write(&main, exercise(manifest, "genpkg", false)).unwrap();
        let exe = lib.join("exercise");
        let mut args = vec![main.to_str().unwrap().to_string(), "-o".into(), exe.to_str().unwrap().into()];
        args.extend(extra.iter().cloned());
        ext.iter().for_each(|e| args.extend(["--extern".into(), e.clone()]));
        args.extend(["--extern".into(), format!("genpkg={}", package.display()), "-L".into(), format!("dependency={}", lib.display())]);
        let args: Vec<&str> = args.iter().map(String::as_str).collect();
        rustc(&args)
    };
    let good = build("");
    assert!(good.status.success(), "{key}: {}", String::from_utf8_lossy(&good.stderr));
    let run = Command::new(dir.join("stub/exercise")).output().unwrap();
    assert!(run.status.success(), "{key}: exercise failed: {}", String::from_utf8_lossy(&run.stderr));
    if !stubs.is_empty() {
        assert!(!build("-mismatch").status.success(), "{key}: a provider stub with a different marker method must fail to compile");
    }
}

fn cargo(args: &[&str], manifest: &Path, target: &Path) -> Output {
    Command::new("cargo").args(args).args(["--offline", "--manifest-path", manifest.to_str().unwrap()]).env("CARGO_TARGET_DIR", target).output().unwrap()
}

fn cargo_check(key: &str, dir: &Path, emit_args: &[&str], bindings_src: &str) -> Vec<String> {
    let crate_dir = dir.join("cargo");
    let mut args = emit_args.to_vec();
    let out = crate_dir.to_str().unwrap();
    args.extend(["--output", out]);
    let r = bin(&args);
    assert!(r.status.success(), "{key}: {}", String::from_utf8_lossy(&r.stderr));
    let manifest = Path::new(out).join("Cargo.toml");
    let text = fs::read_to_string(&manifest).unwrap();
    let imports = yaml(&crate_dir.join("runtimeconditions/runtimeconditions.bindings.yaml"))["importedMarkerContracts"].as_array().unwrap().clone();
    let mut rewritten = String::new();
    for line in text.lines() {
        match line.strip_prefix("rc-dep-").and_then(|l| l.split_once(" = ")) {
            Some((n, req)) => {
                let name = format!("rc-dep-{n}");
                let stub = dir.join("cargo-stubs").join(&name);
                fs::create_dir_all(stub.join("src")).unwrap();
                fs::write(stub.join("Cargo.toml"), format!("[package]\nname = \"{name}\"\nversion = \"1.0.0\"\nedition = \"2021\"\n")).unwrap();
                let traits: String = imports
                    .iter()
                    .filter(|c| c["providerPackage"] == name.as_str())
                    .map(|c| format!("pub trait {} {{ fn {}(&self); }}\n", c["markerInterface"].as_str().unwrap(), c["markerMethod"].as_str().unwrap()))
                    .collect();
                fs::write(stub.join("src/lib.rs"), traits).unwrap();
                rewritten += &format!("{name} = {{ version = {req}, path = {:?} }}\n", stub);
            }
            None => rewritten += &format!("{line}\n"),
        }
    }
    fs::write(&manifest, rewritten).unwrap();
    let target = env::temp_dir().join(format!("rc-rust-emitter-target-{}", std::process::id()));
    let uses_json = bindings_src.contains("serde_json");
    let r = cargo(&[if uses_json { "build" } else { "check" }], &manifest, &target);
    let with_serde = cargo(&["check", "--features", "serde"], &manifest, &target);
    assert!(with_serde.status.success(), "{key}: cargo check --features serde failed: {}", String::from_utf8_lossy(&with_serde.stderr));
    assert!(r.status.success(), "{key}: cargo failed: {}", String::from_utf8_lossy(&r.stderr));
    if !uses_json {
        return vec![];
    }
    let deps = target.join("debug/deps");
    let rlib = fs::read_dir(&deps).unwrap().map(|d| d.unwrap().path()).find(|p| p.file_name().unwrap().to_string_lossy().starts_with("libserde_json-") && p.extension().is_some_and(|e| e == "rlib")).unwrap();
    vec!["-L".into(), format!("dependency={}", deps.display()), "--extern".into(), format!("serde_json={}", rlib.display())]
}

fn check_model(key: &str, model: &Path, validator: &jsonschema::Validator) -> String {
    let dir = scratch(key);
    fs::create_dir_all(&dir).unwrap();
    let t = target(&yaml(model), key, &dir);
    let (m, t) = (model.to_str().unwrap(), t.to_str().unwrap());
    let outs: Vec<PathBuf> = (0..3).map(|i| dir.join(format!("out{i}"))).collect();
    for o in &outs {
        let r = bin(&["emit", "--model", m, "--package-config", t, "--output", o.to_str().unwrap()]);
        assert!(r.status.success(), "{key}: {}", String::from_utf8_lossy(&r.stderr));
    }
    let read = |o: &Path, f: &str| fs::read_to_string(o.join(f)).unwrap();
    let tree = |o: &Path| -> BTreeMap<String, String> {
        fn walk(d: &Path, base: &Path, out: &mut BTreeMap<String, String>) {
            for e in fs::read_dir(d).unwrap() {
                let p = e.unwrap().path();
                if p.is_dir() {
                    walk(&p, base, out);
                } else {
                    out.insert(p.strip_prefix(base).unwrap().to_string_lossy().into_owned(), fs::read_to_string(&p).unwrap());
                }
            }
        }
        let mut out = BTreeMap::new();
        walk(o, o, &mut out);
        out
    };
    assert_eq!(tree(&outs[0]), tree(&outs[1]), "{key}");
    assert_eq!(tree(&outs[0]), tree(&outs[2]), "{key}");
    let o0 = outs[0].to_str().unwrap();
    for f in tree(&outs[0]).keys().filter(|f| f.ends_with(".rs")) {
        let r = Command::new("rustfmt").args(["--check", "--edition", "2021", outs[0].join(f).to_str().unwrap()]).output().unwrap();
        assert!(r.status.success(), "{key}: {f} is not rustfmt-stable: {}", String::from_utf8_lossy(&r.stdout));
    }
    let ok = bin(&["verify", "--model", m, "--package-config", t, "--input", o0]);
    assert!(ok.status.success(), "{key}: {}", String::from_utf8_lossy(&ok.stderr));
    assert!(bin(&["api", "--input", o0]).status.success());
    let assembled = outs[2].join("runtimeconditions/runtimeconditions.binding-model.yaml");
    fs::write(&assembled, "assembled\n").unwrap();
    fs::write(outs[2].join("runtimeconditions.file-manifest.yaml"), "assembled\n").unwrap();
    let after = bin(&["verify", "--model", m, "--package-config", t, "--input", outs[2].to_str().unwrap()]);
    assert!(after.status.success(), "{key}: assembly files must not fail verify: {}", String::from_utf8_lossy(&after.stderr));
    let manifest = yaml(&outs[0].join("runtimeconditions/runtimeconditions.bindings.yaml"));
    let errs: Vec<String> = validator.iter_errors(&manifest).map(|x| format!("{}: {x}", x.instance_path)).collect();
    assert!(errs.is_empty(), "{key}: {errs:?}");

    let src: String = tree(&outs[0]).iter().filter(|(f, _)| f.starts_with("src/")).map(|(_, c)| c.as_str()).collect();
    let extra = cargo_check(key, &dir, &["emit", "--model", m, "--package-config", t], &src);
    if !src.contains("serde_json") || !extra.is_empty() {
        compile_and_run(key, &dir, &outs[0], &manifest, &extra);
    }

    fs::write(outs[1].join("src/bindings.rs"), read(&outs[1], "src/bindings.rs") + "pub fn extra() {}\n").unwrap();
    let bad = bin(&["verify", "--model", m, "--package-config", t, "--input", outs[1].to_str().unwrap()]);
    assert!(!bad.status.success(), "{key}: tampered output accepted");
    let again = bin(&["emit", "--model", m, "--package-config", t, "--output", o0]);
    assert!(String::from_utf8_lossy(&again.stderr).contains("RCR3001"));
    if !read(&outs[0], "Cargo.toml").contains("rc-dep-") {
        serde_round_trip(key, &dir, &outs[0], &manifest);
    }
    let _ = fs::remove_dir_all(&dir);
    src
}

fn serde_round_trip(key: &str, dir: &Path, package: &Path, manifest: &Value) {
    let project = dir.join("serde-exercise");
    fs::create_dir_all(project.join("src")).unwrap();
    let cargo_toml = format!("[package]\nname = \"serde-exercise\"\nversion = \"0.0.0\"\nedition = \"2021\"\n\n[dependencies]\ngenpkg = {{ package = \"rc-{key}\", path = {:?}, features = [\"serde\"] }}\nserde_json = \"1\"\n", package);
    fs::write(project.join("Cargo.toml"), cargo_toml).unwrap();
    fs::write(project.join("src/main.rs"), exercise(manifest, "genpkg", true)).unwrap();
    let target = env::temp_dir().join(format!("rc-rust-emitter-target-{}", std::process::id()));
    let r = cargo(&["run"], &project.join("Cargo.toml"), &target);
    assert!(r.status.success(), "{key}: serde round trip failed: {}", String::from_utf8_lossy(&r.stderr));
}

#[test]
fn conformance_models_emit_verify_compile_and_run() {
    let validator = jsonschema::validator_for(&yaml(&root().join("runtimeconditions.binding-manifest.schema.yaml"))).unwrap();
    for (key, model) in models() {
        check_model(&key, &model, &validator);
    }
}

#[test]
fn json_value_fields_compile_and_run() {
    let validator = jsonschema::validator_for(&yaml(&root().join("runtimeconditions.binding-manifest.schema.yaml"))).unwrap();
    let mut model = yaml(&root().join("conformance/expected/01-owned-kind-interface/runtimeconditions.binding-model.yaml"));
    for root in ["schemas", "scopes"] {
        let props = model[root][0]["projection"]["properties"].as_array_mut().unwrap();
        let shape = &mut props.iter_mut().find(|p| p["name"] == "region").unwrap()["shape"];
        *shape = serde_json::json!({"kind": "any", "provenance": shape["provenance"].clone()});
    }
    let dir = scratch("any-model");
    fs::create_dir_all(&dir).unwrap();
    let path = dir.join("model.yaml");
    fs::write(&path, serde_yaml::to_string(&model).unwrap()).unwrap();
    let src = check_model("json-value", &path, &validator);
    assert!(src.contains("serde_json::Value"));
    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn committed_first_party_models_emit_packages_that_compile() {
    let validator = jsonschema::validator_for(&yaml(&root().join("runtimeconditions.binding-manifest.schema.yaml"))).unwrap();
    let bindings = root().join("../../../bindings");
    let Ok(dirs) = fs::read_dir(&bindings) else { return };
    for d in dirs.map(|d| d.unwrap().path()) {
        let model = d.join("go/runtimeconditions.binding-model.yaml");
        if model.exists() {
            check_model(d.file_name().unwrap().to_str().unwrap(), &model, &validator);
        }
    }
}

#[test]
fn negative_diagnostics_match_the_fixtures() {
    let schema = yaml(&root().join("runtimeconditions.binding-model.schema.yaml"));
    let validator = jsonschema::validator_for(&schema).unwrap();
    let fixtures = yaml(&Path::new(env!("CARGO_MANIFEST_DIR")).join("testdata/negative-diagnostics.yaml"));
    assert!(fixtures.as_array().unwrap().len() >= 4);
    for f in fixtures.as_array().unwrap() {
        let case = f["case"].as_str().unwrap();
        let mut model = yaml(&root().join(format!("conformance/expected/{}/runtimeconditions.binding-model.yaml", f["positiveCase"].as_str().unwrap())));
        match case {
            "reserved-global" => model["schemas"][0]["projection"]["properties"][1]["name"] = "option".into(),
            "unnameable-field" => model["schemas"][0]["projection"]["properties"][1]["name"] = "\u{1f4a5}".into(),
            "fixed-function-collision" => {
                let mut second = model["vocabulary"]["ownedDeclarations"][0].clone();
                second["coordinate"] = "kind:Service".into();
                second["kind"] = "Service".into();
                model["vocabulary"]["ownedDeclarations"].as_array_mut().unwrap().push(second);
            }
            "enum-member-collision" => {
                let at = |m: &Value, root: &str| m[root][0]["projection"]["properties"].as_array().unwrap().iter().position(|x| x["name"] == "mode").unwrap();
                let (i, j) = (at(&model, "schemas"), at(&model, "scopes"));
                let shape = &mut model["schemas"][0]["projection"]["properties"][i]["shape"];
                let first = shape["values"][0]["value"].as_str().unwrap().to_uppercase();
                shape["values"].as_array_mut().unwrap().push(serde_json::json!({"value": first}));
                let copy = shape.clone();
                model["scopes"][0]["projection"]["properties"][j]["shape"] = copy;
            }
            _ => panic!("{case}"),
        }
        let errs: Vec<String> = validator.iter_errors(&model).map(|x| x.to_string()).collect();
        assert!(errs.is_empty(), "{case}: the mutated model must stay schema-valid: {errs:?}");
        let dir = scratch(case);
        fs::create_dir_all(&dir).unwrap();
        let t = target(&model, f["positiveCase"].as_str().unwrap(), &dir);
        let mp = dir.join("model.yaml");
        fs::write(&mp, serde_yaml::to_string(&model).unwrap()).unwrap();
        let r = bin(&["emit", "--model", mp.to_str().unwrap(), "--package-config", t.to_str().unwrap(), "--output", dir.join("out").to_str().unwrap()]);
        assert!(!r.status.success(), "{case}: accepted");
        assert_eq!(String::from_utf8_lossy(&r.stderr).trim_end(), f["message"].as_str().unwrap(), "{case}");
        let _ = fs::remove_dir_all(&dir);
    }
}

#[test]
fn invalid_inputs_are_rejected_with_codes() {
    let (key, model) = models().remove(0);
    let dir = scratch("negative");
    fs::create_dir_all(&dir).unwrap();
    let good = fs::read_to_string(target(&yaml(&model), &key, &dir)).unwrap();
    let text = fs::read_to_string(&model).unwrap();
    let cases: Vec<(&str, String, String, &str)> = vec![
        ("bom", format!("\u{feff}{text}"), good.clone(), "RCR1002"),
        ("crlf", text.replace('\n', "\r\n"), good.clone(), "RCR1002"),
        ("comment", format!("# note\n{text}"), good.clone(), "RCR1002"),
        ("version", text.clone(), good.replace("version: 1.0.0", "version: 1.0.0-snapshot"), "RCR1006"),
        ("sourcedir", text.clone(), good.replace("/rust\n", "/rs\n"), "RCR1007"),
        ("msrv", text.clone(), good.replace("'1.75'", "'1.60'"), "RCR1008"),
        ("root", text.clone(), good.replace("rootExtension: ", "rootExtension: x"), "RCR1009"),
        ("schema", text.replace("semanticSha256:", "semanticShaX:"), good.clone(), "RCR1003"),
        ("apiversion", text.replace("binding-model/v1alpha1", "binding-model/v9"), good.clone(), "RCR1004"),
    ];
    for (n, model_text, target_text, code) in cases {
        let (mp, tp) = (dir.join(format!("{n}.model.yaml")), dir.join(format!("{n}.target.yaml")));
        fs::write(&mp, model_text).unwrap();
        fs::write(&tp, target_text).unwrap();
        let r = bin(&["emit", "--model", mp.to_str().unwrap(), "--package-config", tp.to_str().unwrap(), "--output", dir.join(format!("{n}.out")).to_str().unwrap()]);
        assert!(!r.status.success(), "{n}: accepted");
        assert!(String::from_utf8_lossy(&r.stderr).contains(code), "{n}: {}", String::from_utf8_lossy(&r.stderr));
    }
    let _ = fs::remove_dir_all(&dir);
}

fn rename(model: &mut Value, from: &str, to: &str) {
    match model {
        Value::String(s) if s == from => *s = to.to_string(),
        Value::Array(a) => a.iter_mut().for_each(|x| rename(x, from, to)),
        Value::Object(m) => m.iter_mut().filter(|(k, _)| !matches!(k.as_str(), "exact" | "provenance" | "vocabulary")).for_each(|(_, x)| rename(x, from, to)),
        _ => {}
    }
}

fn emit_renamed(from: &str, to: &str) -> (String, String) {
    let validator = jsonschema::validator_for(&yaml(&root().join("runtimeconditions.binding-manifest.schema.yaml"))).unwrap();
    let mut model = yaml(&root().join("conformance/expected/01-owned-kind-interface/runtimeconditions.binding-model.yaml"));
    rename(&mut model["schemas"], from, to);
    rename(&mut model["scopes"], from, to);
    static NEXT: std::sync::atomic::AtomicUsize = std::sync::atomic::AtomicUsize::new(0);
    let id = NEXT.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
    let dir = scratch(&format!("rename-{id}"));
    fs::create_dir_all(&dir).unwrap();
    let path = dir.join("model.yaml");
    fs::write(&path, serde_yaml::to_string(&model).unwrap()).unwrap();
    let src = check_model(&format!("renamed-{id}"), &path, &validator);
    let _ = fs::remove_dir_all(&dir);
    (src, to.to_string())
}

#[test]
fn keyword_names_become_raw_identifiers_that_compile_and_run() {
    let (src, _) = emit_renamed("endpoint", "type");
    assert!(src.contains("pub r#type: String"), "{src}");
    let (src, _) = emit_renamed("endpoint", "self");
    assert!(src.contains("pub self_: String"), "{src}");
}

#[test]
fn compatibility_forms_are_normalized_before_naming() {
    let (src, _) = emit_renamed("endpoint", "\u{ff35}\u{ff32}\u{ff2c}");
    assert!(src.contains("pub url: String"), "{src}");
}

#[test]
fn diagnostics_are_structured_with_json_pointers() {
    let dir = scratch("diagnostics");
    fs::create_dir_all(&dir).unwrap();
    let (key, model) = models().remove(0);
    let good = fs::read_to_string(target(&yaml(&model), &key, &dir)).unwrap();
    let tp = dir.join("bad.yaml");
    fs::write(&tp, good.replace("version: 1.0.0", "version: nope")).unwrap();
    let out = dir.join("out");
    let args = ["emit", "--model", model.to_str().unwrap(), "--package-config", tp.to_str().unwrap(), "--output", out.to_str().unwrap()];
    let plain = bin(&args);
    assert!(String::from_utf8_lossy(&plain.stderr).starts_with(&format!("RCR1006 {key} /version: ")));
    let mut json_args = vec!["--json"];
    json_args.extend(args);
    let r = bin(&json_args);
    assert!(!r.status.success());
    let d: Value = serde_json::from_slice(&r.stderr).unwrap();
    assert_eq!(d["error"]["code"], "RCR1006");
    assert_eq!(d["error"]["category"], "package-config");
    assert_eq!(d["error"]["coordinate"], key.as_str());
    assert_eq!(d["error"]["jsonPointer"], "/version");

    let mut bad = yaml(&model);
    bad["metadata"]["semanticSha256"] = "short".into();
    let mp = dir.join("bad-model.yaml");
    fs::write(&mp, serde_yaml::to_string(&bad).unwrap()).unwrap();
    let (tp2, o2) = (target(&yaml(&model), &key, &dir), dir.join("o2"));
    let r = bin(&["--json", "emit", "--model", mp.to_str().unwrap(), "--package-config", tp2.to_str().unwrap(), "--output", o2.to_str().unwrap()]);
    let d: Value = serde_json::from_slice(&r.stderr).unwrap();
    assert_eq!(d["error"]["code"], "RCR1003");
    assert_eq!(d["error"]["category"], "model");
    assert_eq!(d["error"]["jsonPointer"], "/metadata/semanticSha256");
    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn scalar_and_chained_definitions_resolve_to_their_types() {
    let validator = jsonschema::validator_for(&yaml(&root().join("runtimeconditions.binding-manifest.schema.yaml"))).unwrap();
    let mut model = yaml(&root().join("conformance/expected/01-owned-kind-interface/runtimeconditions.binding-model.yaml"));
    let coordinate = model["schemas"][0]["coordinate"].as_str().unwrap().to_string();
    let base = model["schemas"][0]["projection"]["properties"][1]["shape"]["provenance"].clone();
    let at = |pointer: &str| {
        let mut p = base.clone();
        p["jsonPointer"] = pointer.into();
        p
    };
    let definition = |name: &str, shape: Value| serde_json::json!({"name": name, "jsonPointer": format!("/$defs/{name}"), "shape": shape, "provenance": at(&format!("/$defs/{name}"))});
    let reference = |name: &str| serde_json::json!({"kind": "ref", "ref": format!("#/$defs/{name}"), "provenance": {"owner": base["owner"], "extensionSha256": base["extensionSha256"], "coordinate": coordinate, "jsonPointer": "/properties/region"}});
    model["schemas"][0]["definitions"] = serde_json::json!([
        definition("label", serde_json::json!({"kind": "scalar", "scalar": "string", "provenance": at("/$defs/label")})),
        definition("alias", reference("label")),
        definition("blob", serde_json::json!({"kind": "any", "provenance": at("/$defs/blob")})),
    ]);
    for root in ["schemas", "scopes"] {
        let props = model[root][0]["projection"]["properties"].as_array_mut().unwrap();
        props.iter_mut().find(|p| p["name"] == "region").unwrap()["shape"] = reference("alias");
    }
    model["schemas"][0]["projection"]["properties"].as_array_mut().unwrap().iter_mut().find(|p| p["name"] == "interface").unwrap()["shape"]["properties"][0]["shape"] = reference("blob");
    let dir = scratch("scalar-definitions-model");
    fs::create_dir_all(&dir).unwrap();
    let path = dir.join("model.yaml");
    fs::write(&path, serde_yaml::to_string(&model).unwrap()).unwrap();
    let src = check_model("scalar-definitions", &path, &validator);
    assert!(src.contains("pub value: String"), "{src}");
    assert!(src.contains("serde_json::Value"), "{src}");
    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn oversized_inputs_are_rejected_before_they_are_read_into_memory() {
    let dir = scratch("oversized");
    fs::create_dir_all(&dir).unwrap();
    let (key, model) = models().remove(0);
    let config = target(&yaml(&model), &key, &dir);
    let big = dir.join("big.yaml");
    fs::File::create(&big).unwrap().set_len((64 << 20) + 1).unwrap();
    let out = dir.join("out");
    let r = bin(&["emit", "--model", big.to_str().unwrap(), "--package-config", config.to_str().unwrap(), "--output", out.to_str().unwrap()]);
    assert!(!r.status.success());
    let stderr = String::from_utf8_lossy(&r.stderr);
    assert!(stderr.starts_with("RCR1001 "), "{stderr}");
    assert!(stderr.contains("exceeds the size limit"), "{stderr}");
    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn transitive_markers_compile_through_the_middle_crate() {
    let model = yaml(&root().join("conformance/expected/03-transitive-closure/runtimeconditions.binding-model.yaml"));
    let root_id = model["rootExtension"]["id"].as_str().unwrap().to_string();
    let leaf_id = model["vocabulary"]["importedDeclarations"][0]["owner"].as_str().unwrap().to_string();
    let extensions = model["extensions"].as_array().unwrap();
    let extension = |id: &str| extensions.iter().find(|x| x["id"] == id).unwrap().clone();
    let middle_id = extensions.iter().find(|x| x["dependencies"].as_array().is_some_and(|d| d.iter().any(|y| *y == leaf_id.as_str()))).unwrap()["id"].as_str().unwrap().to_string();
    let identity = |id: &str| {
        let e = extension(id);
        serde_json::json!({"id": e["id"], "version": e["version"], "semanticSha256": e["semanticSha256"]})
    };

    let mut leaf = model.clone();
    leaf["rootExtension"] = identity(&leaf_id);
    leaf["extensions"] = serde_json::json!([extension(&leaf_id)]);
    leaf["vocabulary"]["ownedDeclarations"] = model["vocabulary"]["importedDeclarations"].clone();
    leaf["vocabulary"]["importedDeclarations"] = serde_json::json!([]);
    let mut middle = model.clone();
    middle["rootExtension"] = identity(&middle_id);
    middle["extensions"] = serde_json::json!([extension(&leaf_id), extension(&middle_id)]);
    middle["vocabulary"]["ownedDeclarations"] = serde_json::json!([]);

    let dir = scratch("transitive-chain");
    fs::create_dir_all(&dir).unwrap();
    let packages = [("leaf", &leaf, &leaf_id, None), ("middle", &middle, &middle_id, Some(("leaf", &leaf_id))), ("root", &model, &root_id, Some(("middle", &middle_id)))];
    for (name, m, id, dependency) in packages {
        let dependencies: Vec<Value> = dependency.iter().map(|(crate_name, extension)| serde_json::json!({"extension": extension, "crateName": format!("rc-chain-{crate_name}"), "version": "1.0.0"})).collect();
        let config = serde_json::json!({
            "apiVersion": "runtimeconditions.io/rust-package-target/v1alpha1", "kind": "RuntimeConditionsRustPackageTarget",
            "packageKey": format!("chain-{name}"), "rootExtension": id, "crateName": format!("rc-chain-{name}"), "version": "1.0.0",
            "sourceDirectory": format!("bindings/chain-{name}/rust"), "minimumRustVersion": "1.75", "publicationMode": "github-tag",
            "repositoryUrl": "https://github.com/runtimeconditions/extensions", "emitterSha256": "0".repeat(64), "dependencies": dependencies,
        });
        let (model_path, config_path, out) = (dir.join(format!("{name}.model.yaml")), dir.join(format!("{name}.target.yaml")), dir.join(name));
        fs::write(&model_path, serde_yaml::to_string(m).unwrap()).unwrap();
        fs::write(&config_path, serde_yaml::to_string(&config).unwrap()).unwrap();
        let r = bin(&["emit", "--model", model_path.to_str().unwrap(), "--package-config", config_path.to_str().unwrap(), "--output", out.to_str().unwrap()]);
        assert!(r.status.success(), "{name}: {}", String::from_utf8_lossy(&r.stderr));
        let manifest = out.join("Cargo.toml");
        let text = fs::read_to_string(&manifest).unwrap();
        let rewritten: String = text
            .lines()
            .map(|line| match line.strip_prefix("rc-chain-").and_then(|l| l.split_once(" = ")) {
                Some((dep, req)) => format!("rc-chain-{dep} = {{ version = {req}, path = {:?} }}\n", dir.join(dep)),
                None => format!("{line}\n"),
            })
            .collect();
        fs::write(&manifest, rewritten).unwrap();
    }
    let middle_facade = fs::read_to_string(dir.join("middle/src/bindings.rs")).unwrap();
    assert!(middle_facade.contains("pub use rc_chain_leaf::WorkerField;"), "{middle_facade}");
    let target = env::temp_dir().join(format!("rc-rust-emitter-target-{}", std::process::id()));
    for name in ["leaf", "middle", "root"] {
        let r = cargo(&["check"], &dir.join(name).join("Cargo.toml"), &target);
        assert!(r.status.success(), "{name}: {}", String::from_utf8_lossy(&r.stderr));
    }
    let _ = fs::remove_dir_all(&dir);
}
