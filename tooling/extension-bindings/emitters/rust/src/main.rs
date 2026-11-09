mod common;
mod diag;
mod input;
mod manifest;
mod naming;
mod plan;
mod render;
mod verify;
mod yaml;

use serde_json::json;
use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::{env, fs, path::Path, process::exit};
use crate::common::*;
use crate::diag::*;
use crate::input::*;
use crate::plan::*;
use crate::render::*;
use crate::verify::*;

fn run(a: &[String]) -> R<String> {
    let usage = || ep("RCR0001", "usage", "", format!("usage: {NAME} [--json] emit|verify|api --model FILE --package-config FILE (--output DIR | --input DIR)"));
    let cmd = a.first().map(String::as_str).unwrap_or("");
    let mut o: HashMap<&str, &str> = HashMap::new();
    if !["emit", "verify", "api"].contains(&cmd) || a.len().is_multiple_of(2) {
        return Err(usage());
    }
    for kv in a[1..].chunks(2) {
        if !["--model", "--package-config", "--output", "--input"].contains(&kv[0].as_str()) || o.insert(&kv[0], &kv[1]).is_some() {
            return Err(usage());
        }
    }
    let need = |k: &str| o.get(k).copied().ok_or_else(usage);
    if cmd == "api" {
        let dir = Path::new(need("--input")?);
        let mut have = BTreeMap::new();
        read_tree(dir, dir, &mut have)?;
        return Ok(json!({"api": api(&have)?}).to_string());
    }
    let (mp, tp) = (need("--model")?, need("--package-config")?);
    let m = strict(&read_limited(mp)?, mp)?;
    if s(&m, "apiVersion") != "runtimeconditions.io/binding-model/v1alpha1" {
        return Err(e("RCR1004", mp, "unsupported binding-model apiVersion"));
    }
    if s(&m, "kind") != "RuntimeConditionsBindingModel" {
        return Err(e("RCR1005", mp, "unsupported binding-model kind"));
    }
    validate(&m, mp)?;
    let t = load_target(tp)?;
    let key = s(&t, "packageKey");
    let files = render(&m, &t, &build(&m, &t)?)?;
    if cmd == "verify" {
        let dir = Path::new(need("--input")?);
        let mut have = BTreeMap::new();
        read_tree(dir, dir, &mut have)?;
        for assembled in ["binding-model", "binding-release"] {
            have.remove(&format!("runtimeconditions/runtimeconditions.{assembled}.yaml"));
        }
        have.remove("runtimeconditions/runtimeconditions.extension.yaml");
        have.remove("runtimeconditions.file-manifest.yaml");
        if have != files {
            let diff: Vec<&String> = have.keys().chain(files.keys()).filter(|k| have.get(*k) != files.get(*k)).collect::<BTreeSet<_>>().into_iter().collect();
            return Err(e("RCR4002", key, format!("generated package differs from a fresh rendering: {diff:?}")));
        }
        return Ok(json!({"api": api(&have)?, "modelMapping": format!("verified {} files", have.len())}).to_string());
    }
    let out = Path::new(need("--output")?);
    if out.is_symlink() || (out.exists() && (!out.is_dir() || out.read_dir().map_err(|_| e("RCR3002", key, "cannot inspect output directory"))?.next().is_some())) {
        return Err(e("RCR3001", key, "output directory must be new or empty and not a symbolic link"));
    }
    for (f, c) in &files {
        let d = out.join(f);
        fs::create_dir_all(d.parent().unwrap()).and_then(|_| fs::write(&d, c)).map_err(|_| e("RCR3002", key, "cannot write generated package files"))?;
    }
    Ok(json!({"output": out}).to_string())
}

fn main() {
    let mut a: Vec<String> = env::args().skip(1).collect();
    let json = a.iter().any(|x| x == "--json");
    a.retain(|x| x != "--json");
    if a == ["--version"] {
        return println!("{VERSION}");
    }
    match run(&a) {
        Ok(x) => println!("{x}"),
        Err(d) => {
            if json {
                eprintln!("{}", json!({"error": {"category": d.category, "code": d.code, "coordinate": d.coordinate, "jsonPointer": d.pointer, "message": d.message}}));
            } else {
                eprintln!("{d}");
            }
            exit(1);
        }
    }
}
