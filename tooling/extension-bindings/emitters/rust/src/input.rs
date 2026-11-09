use serde_json::Value;
use std::collections::HashSet;
use std::{env, fs};
use crate::common::*;
use crate::diag::*;

pub const MAX_BYTES: u64 = 64 << 20;

pub fn read_limited(path: &str) -> R<String> {
    use std::io::Read;
    let mut buf = Vec::new();
    fs::File::open(path).and_then(|f| f.take(MAX_BYTES + 1).read_to_end(&mut buf)).map_err(|x| e("RCR1001", path, x))?;
    if buf.len() as u64 > MAX_BYTES {
        return Err(e("RCR1001", path, "exceeds the size limit"));
    }
    String::from_utf8(buf).map_err(|_| e("RCR1002", path, "must be UTF-8"))
}

pub fn semver(v: &str) -> Option<(u64, u64, u64)> {
    let (core, pre) = v.split_once('-').map_or((v, None), |(c, p)| (c, Some(p)));
    let ok = |x: &str| !x.is_empty() && x.bytes().all(|b| b.is_ascii_digit()) && (x == "0" || !x.starts_with('0'));
    if let Some(p) = pre {
        let (k, n) = p.split_once('.')?;
        if !["alpha", "beta", "rc"].contains(&k) || !ok(n) {
            return None;
        }
    }
    let p: Vec<&str> = core.split('.').collect();
    (p.len() == 3 && p.iter().all(|x| ok(x))).then(|| (p[0].parse().unwrap(), p[1].parse().unwrap(), p[2].parse().unwrap()))
}

pub fn load_target(p: &str) -> R<Value> {
    let t = strict(&read_limited(p)?, p)?;
    let k = s(&t, "packageKey");
    let bad = |f: &str, m: &str| ep("RCR1006", k, &format!("/{f}"), m);
    if s(&t, "apiVersion") != "runtimeconditions.io/rust-package-target/v1alpha1" || s(&t, "kind") != "RuntimeConditionsRustPackageTarget" {
        return Err(bad("apiVersion", "unsupported target apiVersion or kind"));
    }
    for f in ["packageKey", "rootExtension", "crateName", "version", "sourceDirectory", "minimumRustVersion", "publicationMode", "repositoryUrl"] {
        if s(&t, f).is_empty() {
            return Err(bad(f, "required string is missing"));
        }
    }
    let crate_ok = |c: &str| c.starts_with(|x: char| x.is_ascii_lowercase()) && c.bytes().all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-' || b == b'_');
    if !crate_ok(s(&t, "crateName")) {
        return Err(bad("crateName", "invalid crate name"));
    }
    if semver(s(&t, "version")).is_none() {
        return Err(bad("version", "unsupported package version"));
    }
    if s(&t, "sourceDirectory") != format!("bindings/{k}/rust") {
        return Err(ep("RCR1007", k, "/sourceDirectory", "source directory does not match package key"));
    }
    let msrv: Vec<u32> = s(&t, "minimumRustVersion").split('.').filter_map(|x| x.parse().ok()).collect();
    if msrv.len() != 2 || (msrv[0], msrv[1]) < (1, 70) {
        return Err(ep("RCR1008", k, "/minimumRustVersion", "minimum Rust version must be <major>.<minor> and at least 1.70"));
    }
    if !s(&t, "repositoryUrl").starts_with("https://") {
        return Err(bad("repositoryUrl", "must use HTTPS"));
    }
    match (s(&t, "publicationMode"), t.get("registryId").is_some()) {
        ("registry", true) | ("github-tag", false) => {}
        _ => return Err(bad("registryId", "required exactly for registry publication")),
    }
    let sha = s(&t, "emitterSha256");
    if sha.len() != 64 || !sha.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)) {
        return Err(bad("emitterSha256", "must be a 64-character lowercase SHA-256"));
    }
    let mut seen = HashSet::new();
    for d in arr(&t, "dependencies") {
        if s(d, "extension").is_empty() || !crate_ok(s(d, "crateName")) || semver(s(d, "version")).is_none() || !seen.insert(s(d, "crateName")) {
            return Err(bad("dependencies", "invalid or duplicate direct dependency"));
        }
    }
    Ok(t)
}

pub fn strict(text: &str, at: &str) -> R<Value> {
    let bad = |m: &str| e("RCR1002", at, m);
    if text.len() as u64 > MAX_BYTES {
        return Err(e("RCR1001", at, "exceeds the size limit"));
    }
    if text.starts_with('\u{feff}') || text.contains('\r') || !text.ends_with('\n') || text.ends_with("\n\n") {
        return Err(bad("must be UTF-8 without BOM, LF line endings and one trailing newline"));
    }
    for l in text.lines() {
        let (mut q, mut prev, mut ns) = (None, ' ', true);
        for c in l.chars() {
            if let Some(k) = q {
                q = (c != k).then_some(k);
            } else {
                if ns && (c == '"' || c == '\'') {
                    q = Some(c);
                } else if (ns && "&*!".contains(c)) || (c == '#' && (ns || prev == ' ')) {
                    return Err(bad("comments, anchors, aliases and tags are not allowed"));
                }
                ns = (ns && c == ' ') || ":-,[{".contains(c);
            }
            prev = c;
        }
    }
    fn conv(v: serde_yaml::Value, d: usize, n: &mut usize) -> Result<Value, &'static str> {
        use serde_yaml::Value as Y;
        *n += 1;
        if d > 64 || *n > 1_000_000 {
            return Err("exceeds the node or depth limit");
        }
        Ok(match v {
            Y::Null => Value::Null,
            Y::Bool(b) => b.into(),
            Y::Number(x) if x.as_f64().is_some_and(|f| !f.is_finite()) => return Err("numbers must be finite"),
            Y::Number(x) => serde_json::to_value(x).map_err(|_| "unrepresentable number")?,
            Y::String(s) => s.into(),
            Y::Sequence(a) => Value::Array(a.into_iter().map(|x| conv(x, d + 1, n)).collect::<Result<_, _>>()?),
            Y::Mapping(m) => Value::Object(
                m.into_iter()
                    .map(|(k, x)| match k {
                        Y::String(k) => Ok((k, conv(x, d + 1, n)?)),
                        _ => Err("mapping keys must be strings"),
                    })
                    .collect::<Result<_, _>>()?,
            ),
            Y::Tagged(_) => return Err("tags are not allowed"),
        })
    }
    conv(serde_yaml::from_str(text).map_err(|x| bad(&x.to_string()))?, 0, &mut 0).map_err(bad)
}

pub fn validate(m: &Value, at: &str) -> R<()> {
    let text = match env::var("RC_BINDING_MODEL_SCHEMA") {
        Ok(p) => read_limited(&p)?,
        Err(_) => include_str!("../../../model/runtimeconditions.binding-model.schema.yaml").into(),
    };
    let schema: Value = serde_yaml::from_str(&text).map_err(|x| e("RCR1003", at, x))?;
    let v = jsonschema::validator_for(&schema).map_err(|x| e("RCR1003", at, x))?;
    let found: Vec<(String, String)> = v.iter_errors(m).map(|x| (x.instance_path.to_string(), x.to_string())).collect();
    match found.first() {
        None => Ok(()),
        Some((pointer, _)) => Err(ep("RCR1003", at, pointer, found.iter().map(|(p, x)| format!("{p}: {x}")).collect::<Vec<_>>().join("; "))),
    }
}
