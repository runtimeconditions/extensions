use serde_json::Value;
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet, HashMap, HashSet};
use crate::common::*;
use crate::diag::*;
use crate::input::*;
use crate::manifest::*;
use crate::naming::*;
use crate::plan::*;

const DERIVE: &str = "#[derive(Clone, Debug, PartialEq)]";
const SERDE: &str = "#[cfg_attr(feature = \"serde\", derive(serde::Serialize, serde::Deserialize))]";

pub fn ann(t: &Ty, n: &HashMap<String, String>) -> String {
    match t {
        Ty::Prim(p) => p.to_string(),
        Ty::Named(k) => n[k].clone(),
    }
}

fn held(t: &Ty, owner: &str, n: &HashMap<String, String>, boxed: &HashSet<(String, String)>) -> String {
    match t {
        Ty::Named(k) if boxed.contains(&(owner.to_string(), k.clone())) => format!("Box<{}>", n[k]),
        _ => ann(t, n),
    }
}

pub fn module_ident(type_name: &str) -> R<String> {
    name(type_name, St::Snake, type_name)
}

pub fn module_file(type_name: &str) -> String {
    let ident = module_ident(type_name).unwrap_or_default();
    if ident.is_ascii() {
        ident.trim_start_matches("r#").to_string()
    } else {
        format!("module_{}", &format!("{:x}", Sha256::digest(type_name))[..16])
    }
}

fn origin(prov: &Value) -> String {
    match s(prov, "jsonPointer") {
        "" => s(prov, "coordinate").into(),
        j => format!("{} {j}", s(prov, "coordinate")),
    }
}

pub fn interval(v: &str) -> String {
    let (a, b, _) = semver(v).unwrap();
    format!(">={v}, <{}", if a == 0 { format!("0.{}.0", b + 1) } else { format!("{}.0.0", a + 1) })
}

pub fn rustfmt(src: String) -> R<String> {
    use std::io::Write;
    use std::process::{Command, Stdio};
    let bad = |x: String| e("RCR4003", "rustfmt", x);
    let mut c = Command::new("rustfmt")
        .args(["--edition", "2021", "--config", "newline_style=Unix"])
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|x| bad(x.to_string()))?;
    let mut stdin = c.stdin.take().unwrap();
    let writer = std::thread::spawn(move || stdin.write_all(src.as_bytes()));
    let out = c.wait_with_output().map_err(|x| bad(x.to_string()))?;
    writer.join().unwrap().map_err(|x| bad(x.to_string()))?;
    if !out.status.success() {
        return Err(bad(String::from_utf8_lossy(&out.stderr).into_owned()));
    }
    String::from_utf8(out.stdout).map_err(|x| bad(x.to_string()))
}

fn type_code(key: &str, r: &Raw, n: &HashMap<String, String>, boxed: &HashSet<(String, String)>, imports: &mut BTreeSet<String>) -> String {
    let nm = &n[key];
    let wrapper = matches!(r.body, Body::Obj(_)) && s(&r.shape, "kind") != "object";
    let mut o = format!("/// `{}` generated from `{}`.\n", r.src, origin(&r.shape["provenance"]));
    match &r.body {
        Body::Enm(ms) => {
            o += &format!("#[derive(Clone, Copy, Debug, PartialEq, Eq)]\n{SERDE}\npub enum {nm} {{\n");
            for (k, v) in ms {
                o += &format!("/// Serialized as {v:?}.\n#[cfg_attr(feature = \"serde\", serde(rename = {v:?}))]\n{},\n", n[k]);
            }
            o += &format!("}}\n\nimpl {nm} {{\npub fn value(&self) -> &'static str {{\nmatch self {{\n");
            ms.iter().for_each(|(k, v)| o += &format!("{nm}::{} => {v:?},\n", n[k]));
            o += "}\n}\n}\n";
        }
        Body::Arr(x) => o += &format!("{DERIVE}\n{SERDE}\npub struct {nm}(pub Vec<{}>);\n", ann(x, n)),
        Body::Map(x) => o += &format!("{DERIVE}\n{SERDE}\npub struct {nm}(pub std::collections::BTreeMap<String, {}>);\n", ann(x, n)),
        Body::Uni(vs) => {
            o += &format!("{DERIVE}\n{SERDE}\n#[cfg_attr(feature = \"serde\", serde(untagged))]\npub enum {nm} {{\n");
            vs.iter().enumerate().for_each(|(i, x)| o += &format!("Variant{}({}),\n", i + 1, held(x, key, n, boxed)));
            o += "}\n";
        }
        Body::Obj(fs) => {
            let mut fs: Vec<(&Fld, &String)> = fs.iter().map(|f| (f, &n[&f.key])).collect();
            fs.sort_by(|a, b| (!a.0.req, a.1).cmp(&(!b.0.req, b.1)));
            o += &format!("{DERIVE}\n{SERDE}\n");
            if wrapper {
                o += "#[cfg_attr(feature = \"serde\", serde(transparent))]\n";
            }
            o += &format!("pub struct {nm} {{\n");
            for (f, fnm) in &fs {
                let ty = held(&f.ty, key, n, boxed);
                o += &format!("/// `{}` at `{}`.\n", f.src, origin(&f.prov));
                if !wrapper && fnm.trim_start_matches("r#") != f.src {
                    o += &format!("#[cfg_attr(feature = \"serde\", serde(rename = {:?}))]\n", f.src);
                }
                if !f.req {
                    o += "#[cfg_attr(feature = \"serde\", serde(default, skip_serializing_if = \"Option::is_none\"))]\n";
                }
                o += &format!("pub {fnm}: {},\n", if f.req { ty } else { format!("Option<{ty}>") });
            }
            o += "}\n";
            if let Some(mk) = &r.mk {
                if let Some(c) = &mk.krate {
                    imports.insert(format!("{}::{}", c.replace('-', "_"), n[&mk.proto]));
                }
                o += &format!("\nimpl {} for {nm} {{\nfn {}(&self) {{}}\n}}\n", n[&mk.proto], mk.method);
            }
        }
    }
    o
}

pub fn render(m: &Value, t: &Value, p: &Plan) -> R<BTreeMap<String, String>> {
    let n = &p.names;
    let h = header(m);
    let boxed = boxed(&p.raw);
    let mut files = BTreeMap::new();
    let mut facade = format!("// {h}\n\n");
    let mut exports = String::new();
    let mut taken: HashMap<String, String> = HashMap::new();
    let ordered: BTreeMap<&String, (&String, &Raw)> = p.raw.iter().map(|(k, r)| (&n[k], (k, r))).collect();
    for (nm, (key, r)) in ordered {
        let ident = module_ident(nm)?;
        let file = module_file(nm);
        if let Some(other) = taken.insert(file.clone(), nm.clone()) {
            return Err(e("RCR2001", s(t, "packageKey"), format!("types '{other}' and '{nm}' share module '{file}'")));
        }
        let mut imports = BTreeSet::new();
        let code = type_code(key, r, n, &boxed, &mut imports);
        let uses: String = imports.iter().map(|i| format!("use {i};\n")).collect();
        files.insert(format!("src/bindings/{file}.rs"), rustfmt(format!("// {h}\n\n#[allow(unused_imports)]\nuse super::*;\n{uses}\n{code}"))?);
        if !ident.is_ascii() {
            facade += &format!("#[path = \"bindings/{file}.rs\"]\n");
        }
        facade += &format!("mod {ident};\n");
        exports += &format!("pub use {ident}::{nm};\n");
    }
    let reexports: BTreeSet<String> = p.imps.iter().map(|i| format!("pub use {}::{};\n", i.krate.replace('-', "_"), n[&i.proto])).collect();
    facade += &format!("\n{exports}{}", reexports.into_iter().collect::<String>());
    if !p.decls.is_empty() {
        facade += "\n/// An inert value returned by the declaration functions.\n#[derive(Clone, Debug, PartialEq)]\npub struct Declaration(());\n";
    }
    for d in &p.decls {
        facade += &format!("\n/// Marker for fields that belong to the `{}` declaration.\npub trait {} {{\nfn {}(&self);\n}}\n", d.kind, n[&d.proto], d.method);
        facade += &format!("\n/// Declares a `{}` condition from fields that carry its marker.\npub fn {}(_fields: &[&dyn {}]) -> Declaration {{\nDeclaration(())\n}}\n", d.kind, n[&d.func], n[&d.proto]);
    }
    files.insert("src/bindings.rs".into(), rustfmt(facade)?);
    files.insert("src/lib.rs".into(), rustfmt(format!("// {h}\n\n#![allow(clippy::all)]\n\nmod bindings;\n\npub use bindings::*;\n"))?);

    let mut cargo = format!("# {h}\n\n[package]\nname = \"{}\"\nversion = \"{}\"\nedition = \"2021\"\nrust-version = \"{}\"\nrepository = \"{}\"\n", s(t, "crateName"), s(t, "version"), s(t, "minimumRustVersion"), s(t, "repositoryUrl"));
    cargo += "include = [\"src/**\", \"runtimeconditions/**\", \"Cargo.toml\"]\n\n[features]\nserde = [\"dep:serde\"]\n\n[dependencies]\nserde = { version = \"1\", features = [\"derive\"], optional = true }\n";
    if p.any {
        cargo += "serde_json = \"1\"\n";
    }
    let mut deps: Vec<&Value> = arr(t, "dependencies").iter().collect();
    deps.sort_by_key(|d| s(d, "crateName"));
    deps.iter().for_each(|d| cargo += &format!("{} = \"{}\"\n", s(d, "crateName"), interval(s(d, "version"))));
    files.insert("Cargo.toml".into(), cargo);
    files.insert(MANIFEST.into(), manifest(m, t, p, &h)?);
    Ok(files)
}
