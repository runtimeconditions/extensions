use serde_json::Value;
use std::collections::{BTreeMap, BTreeSet, HashMap, HashSet};
use crate::common::*;
use crate::diag::*;
use crate::naming::*;

#[derive(Clone)]
pub enum Ty {
    Prim(&'static str),
    Named(String),
}

#[derive(Clone)]
pub struct Mk {
    pub owner: String,
    pub proto: String,
    pub method: String,
    pub krate: Option<String>,
}

pub struct Fld {
    pub key: String,
    pub src: String,
    pub ty: Ty,
    pub req: bool,
    pub prov: Value,
}

pub enum Body {
    Obj(Vec<Fld>),
    Enm(Vec<(String, String)>),
    Arr(Ty),
    Map(Ty),
    Uni(Vec<Ty>),
}

pub struct Raw {
    pub shape: Value,
    pub body: Body,
    pub mk: Option<Mk>,
    pub src: String,
}

pub struct Decl {
    pub coord: String,
    pub kind: String,
    pub func: String,
    pub proto: String,
    pub method: String,
    pub prov: Value,
}

pub struct Imp {
    pub coord: String,
    pub owner: String,
    pub kind: String,
    pub proto: String,
    pub method: String,
    pub krate: String,
    pub prov: Value,
}

#[derive(Default)]
pub struct B {
    pub syms: Vec<Sym>,
    pub raw: BTreeMap<String, Raw>,
    pub defs: HashMap<(String, String), String>,
    pub used: BTreeSet<String>,
    pub resolved: HashMap<String, Ty>,
    pub any: bool,
}

impl B {
    #[allow(clippy::too_many_arguments)]
    pub fn sym(&mut self, key: &str, at: &str, src: &str, st: St, ns: &str, par: &[String], fixed: bool) {
        let ns = if ns.is_empty() { "module" } else { ns };
        self.syms.push(Sym { key: key.into(), at: at.into(), src: src.into(), st, ns: ns.into(), par: par.into(), fixed });
    }

    pub fn reg(&mut self, sh: &Value, key: &str, src: &str, par: &[String], mk: Option<&Mk>, exact: Option<&str>) -> R<Ty> {
        let at = loc(sh);
        let kind = s(sh, "kind");
        let sn = exact.unwrap_or(src).to_string();
        let put = |b: &mut B, body: Body| {
            b.raw.insert(key.into(), Raw { shape: sh.clone(), body, mk: mk.cloned(), src: sn.clone() });
        };
        match kind {
            "any" => {
                self.any = true;
                Ok(Ty::Prim("serde_json::Value"))
            }
            "scalar" => {
                let vals = arr(sh, "values");
                if vals.is_empty() || s(sh, "scalar") != "string" {
                    return Ok(Ty::Prim(match s(sh, "scalar") {
                        "string" => "String",
                        "boolean" => "bool",
                        "integer" => "i64",
                        "number" => "f64",
                        _ => "()",
                    }));
                }
                self.sym(key, &at, src, St::Pascal, "", par, false);
                let mut ms = vec![];
                for (i, v) in vals.iter().enumerate() {
                    let v = v["value"].as_str().ok_or_else(|| e("RCR1012", &at, "string domain contains non-string value"))?;
                    let mk = format!("member:{key}:{i}");
                    self.sym(&mk, &format!("{at}#value:{v}"), v, St::Pascal, &format!("enum:{key}"), &[], false);
                    ms.push((mk, v.to_string()));
                }
                put(self, Body::Enm(ms));
                Ok(Ty::Named(key.into()))
            }
            "ref" => {
                let r = s(sh, "ref");
                let r = r.strip_prefix('#').unwrap_or(r);
                let k = self.defs.get(&(s(&sh["provenance"], "coordinate").into(), r.into())).cloned();
                let k = k.ok_or_else(|| e("RCR1012", &at, format!("unresolved reference '{}'", s(sh, "ref"))))?;
                self.used.insert(k.clone());
                Ok(Ty::Named(k))
            }
            "object" => {
                self.sym(key, &at, src, St::Pascal, "", par, false);
                let ns = format!("fields:{key}");
                if let Some(m) = mk {
                    self.sym(&format!("method:{key}"), &m.owner, &m.method, St::Exact, &ns, &[], true);
                }
                let props = arr(sh, "properties");
                let names: BTreeSet<&str> = props.iter().map(|p| s(p, "name")).collect();
                let req: BTreeSet<&str> = arr(sh, "required").iter().filter_map(Value::as_str).collect();
                let missing: Vec<&&str> = req.difference(&names).collect();
                if !missing.is_empty() {
                    return Err(e("RCR1012", &at, format!("required properties are missing: {missing:?}")));
                }
                if names.len() != props.len() {
                    return Err(e("RCR1012", &at, "duplicate object property"));
                }
                let mut fs = vec![];
                for p in props {
                    let n = s(p, "name");
                    let fk = format!("field:{key}:{n}");
                    self.sym(&fk, &loc(p), n, St::Snake, &ns, &[], false);
                    let cp: Vec<String> = std::iter::once(src.to_string()).chain(par.iter().cloned()).collect();
                    let ty = self.reg(&p["shape"], &format!("type:{key}:{n}"), n, &cp, None, None)?;
                    let req = req.contains(n) || p["required"].as_bool().unwrap_or(false);
                    fs.push(Fld { key: fk, src: n.into(), ty, req, prov: p["provenance"].clone() });
                }
                put(self, Body::Obj(fs));
                Ok(Ty::Named(key.into()))
            }
            "array" | "map" | "union" => {
                let kids: Vec<&Value> = if kind == "union" { arr(sh, "variants").iter().collect() } else { vec![&sh[if kind == "array" { "items" } else { "mapValues" }]] };
                if kids.is_empty() {
                    return Err(e("RCR1012", &at, "union has no variants"));
                }
                let mut tys = vec![];
                for (i, k) in kids.iter().enumerate() {
                    let suf = match kind { "array" => "Item".into(), "map" => "Value".into(), _ => format!("Variant{}", i + 1) };
                    let base = if kind == "array" { src.strip_suffix(" Value").unwrap_or(src) } else { src };
                    tys.push(self.reg(k, &format!("type:{key}:{suf}"), &format!("{base} {suf}"), par, None, Some(&sn))?);
                }
                self.sym(key, &at, src, St::Pascal, "", par, false);
                let mut tys = tys.into_iter();
                put(self, match kind {
                    "array" => Body::Arr(tys.next().unwrap()),
                    "map" => Body::Map(tys.next().unwrap()),
                    _ => Body::Uni(tys.collect()),
                });
                Ok(Ty::Named(key.into()))
            }
            _ => Err(e("RCR1010", &at, format!("unknown structural node '{kind}'"))),
        }
    }
}

pub struct Plan {
    pub names: HashMap<String, String>,
    pub decls: Vec<Decl>,
    pub imps: Vec<Imp>,
    pub raw: BTreeMap<String, Raw>,
    pub any: bool,
}

pub fn reaches(g: &HashMap<&str, Vec<&str>>, cur: &str, owner: &str, seen: &mut HashSet<String>) -> bool {
    cur == owner
        || (seen.insert(cur.into()) && g.get(cur).is_some_and(|d| d.iter().any(|x| reaches(g, x, owner, seen))))
}

pub fn build(m: &Value, t: &Value) -> R<Plan> {
    let root = s(&m["rootExtension"], "id");
    let pkg = s(t, "packageKey");
    if root != s(t, "rootExtension") {
        return Err(ep("RCR1009", pkg, "/rootExtension", format!("target root '{}' differs from model root '{root}'", s(t, "rootExtension"))));
    }
    let exts = arr(m, "extensions");
    let ext = exts.iter().find(|x| s(x, "id") == root).ok_or_else(|| e("RCR1011", root, "root extension is absent from resolved extension closure"))?;
    let direct: BTreeSet<&str> = arr(ext, "dependencies").iter().filter_map(Value::as_str).collect();
    let deps = arr(t, "dependencies");
    let conf: BTreeSet<&str> = deps.iter().map(|d| s(d, "extension")).collect();
    if direct != conf {
        return Err(ep("RCR1009", pkg, "/dependencies", format!("direct dependency set differs: model={direct:?}, target={conf:?}")));
    }
    let ids: HashSet<&str> = exts.iter().map(|x| s(x, "id")).collect();
    if !conf.iter().all(|c| ids.contains(c)) {
        return Err(e("RCR1011", root, "target dependency is absent from resolved closure"));
    }
    let graph: HashMap<&str, Vec<&str>> =
        exts.iter().map(|x| (s(x, "id"), arr(x, "dependencies").iter().filter_map(Value::as_str).collect())).collect();
    let mut sorted: Vec<&Value> = deps.iter().collect();
    sorted.sort_by_key(|d| s(d, "extension"));

    let mut b = B::default();
    let voc = &m["vocabulary"];
    let (mut decls, mut imps, mut owned, mut imported) = (vec![], vec![], HashMap::new(), HashMap::new());
    for d in arr(voc, "ownedDeclarations") {
        let (c, k) = (s(d, "coordinate"), s(d, "kind"));
        if s(d, "owner") != root {
            return Err(e("RCR1011", c, "owned declaration has a foreign owner"));
        }
        if owned.insert(k, c).is_some() {
            return Err(e("RCR1011", c, format!("duplicate owned kind '{k}'")));
        }
        b.sym(&format!("function:{c}"), c, k, St::Snake, "", &[], true);
        b.sym(&format!("protocol:{c}"), c, &format!("{k} Field"), St::Pascal, "", &[], true);
        decls.push(Decl { coord: c.into(), kind: k.into(), func: format!("function:{c}"), proto: format!("protocol:{c}"), method: marker_method(root, c), prov: d["provenance"].clone() });
    }
    if !decls.is_empty() {
        b.sym("builtin:Declaration", "rust:Declaration", "Declaration", St::Pascal, "", &[], true);
    }
    for d in arr(voc, "importedDeclarations") {
        let (o, c, k) = (s(d, "owner"), s(d, "coordinate"), s(d, "kind"));
        if o == root || !ids.contains(o) {
            return Err(e("RCR1011", c, "invalid imported declaration owner"));
        }
        b.sym(&format!("import:function:{c}"), c, k, St::Snake, "", &[], true);
        b.sym(&format!("import:protocol:{c}"), c, &format!("{k} Field"), St::Pascal, "", &[], true);
        let krate = sorted
            .iter()
            .find(|x| reaches(&graph, s(x, "extension"), o, &mut HashSet::new()))
            .map(|x| s(x, "crateName"))
            .ok_or_else(|| e("RCR1011", o, "imported declaration has no direct dependency provider"))?;
        let imp = Imp { coord: c.into(), owner: o.into(), kind: k.into(), proto: format!("import:protocol:{c}"), method: marker_method(o, c), krate: krate.into(), prov: d["provenance"].clone() };
        if imported.insert(k, (c, imps.len())).is_some() {
            return Err(e("RCR1011", c, "duplicate imported kind"));
        }
        imps.push(imp);
    }

    let fields_of = |k: &str, i: &str, n: &str| (k.to_string(), i.to_string(), n.to_string());
    let roots: HashSet<_> = arr(voc, "conditionFields")
        .iter()
        .filter(|f| s(f, "owner") == root)
        .map(|f| fields_of(s(f, "kind"), s(f, "interfaceType"), s(&f["segments"][0], "name")))
        .collect();
    let scopes: HashMap<_, _> =
        arr(m, "scopes").iter().map(|x| ((s(x, "kind"), s(x, "interfaceType")), &x["projection"])).collect();
    let schemas = arr(m, "schemas");
    for sc in schemas {
        for d in arr(sc, "definitions") {
            b.defs.insert((s(sc, "coordinate").into(), s(d, "jsonPointer").into()), format!("type:{}:{}", s(sc, "coordinate"), s(d, "jsonPointer")));
        }
    }
    let mut pending: BTreeMap<String, (&Value, Vec<String>)> = BTreeMap::new();
    for sc in schemas.iter().filter(|x| s(x, "owner") == root) {
        let sco = s(sc, "coordinate");
        let kind = sc["kind"].as_str().unwrap_or_else(|| s(sc, "id"));
        let iface = s(sc, "interfaceType");
        let scope: Vec<String> = [iface, kind].into_iter().filter(|x| !x.is_empty()).map(String::from).collect();
        let mk = if let Some(c) = owned.get(kind) {
            Some(Mk { owner: root.into(), proto: format!("protocol:{c}"), method: marker_method(root, c), krate: None })
        } else {
            imported.get(kind).map(|(_, i)| {
                let p: &Imp = &imps[*i];
                Mk { owner: p.owner.clone(), proto: p.proto.clone(), method: p.method.clone(), krate: Some(p.krate.clone()) }
            })
        };
        let proj = &sc["projection"];
        if s(proj, "kind") != "object" {
            return Err(e("RCR1012", sco, "root condition projection must be an object"));
        }
        for p in arr(proj, "properties") {
            let n = s(p, "name");
            let is_if = n == "interface" && !iface.is_empty();
            let fsrc = if is_if { iface } else { n };
            let par: Vec<String> = is_if.then(|| n.to_string()).into_iter().chain(scope.iter().cloned()).collect();
            let key = format!("type:field:{sco}:{n}");
            let mut shape = &p["shape"];
            if roots.contains(&fields_of(kind, iface, n)) {
                shape = scopes
                    .get(&(kind, iface))
                    .and_then(|pr| arr(pr, "properties").iter().find(|x| s(x, "name") == n))
                    .map(|x| &x["shape"])
                    .ok_or_else(|| e("RCR1012", sco, format!("condition field '{n}' is absent from its scope projection")))?;
            }
            if s(shape, "kind") == "object" {
                b.reg(shape, &key, fsrc, &par, mk.as_ref(), None)?;
                continue;
            }
            let m = mk.as_ref().ok_or_else(|| e("RCR1011", sco, format!("field '{n}' has no declaration contract")))?;
            let (at, ns, vk) = (loc(p), format!("fields:{key}"), format!("field:{key}:value"));
            b.sym(&key, &at, fsrc, St::Pascal, "", &par, false);
            b.sym(&format!("method:{key}"), &m.owner, &m.method, St::Exact, &ns, &[], true);
            b.sym(&vk, &at, "value", St::Snake, &ns, &[], true);
            let ty = b.reg(shape, &format!("type:{key}:value"), &format!("{fsrc} Value"), &par, None, None)?;
            let f = Fld { key: vk, src: "value".into(), ty, req: true, prov: p["provenance"].clone() };
            b.raw.insert(key, Raw { shape: shape.clone(), body: Body::Obj(vec![f]), mk: mk.clone(), src: fsrc.into() });
        }
        for d in arr(sc, "definitions") {
            let dk = b.defs[&(sco.to_string(), s(d, "jsonPointer").to_string())].clone();
            if arr(proj, "properties").is_empty() {
                b.used.insert(dk.clone());
            }
            pending.insert(dk, (d, scope.clone()));
        }
    }
    loop {
        let need: Vec<String> = b.used.iter().filter(|k| pending.contains_key(*k)).cloned().collect();
        if need.is_empty() {
            break;
        }
        for k in need {
            let (d, sc) = pending.remove(&k).unwrap();
            let ty = b.reg(&d["shape"], &k, s(d, "name"), &sc, None, None)?;
            if !b.raw.contains_key(&k) {
                b.resolved.insert(k, ty);
            }
        }
    }
    let keys: HashSet<String> = b.raw.keys().cloned().collect();
    for r in b.raw.values_mut() {
        match &mut r.body {
            Body::Obj(fs) => {
                for f in fs {
                    f.ty = subst(&f.ty, &b.resolved, &keys, 0)?;
                }
            }
            Body::Arr(t) | Body::Map(t) => *t = subst(t, &b.resolved, &keys, 0)?,
            Body::Uni(vs) => {
                for v in vs {
                    *v = subst(v, &b.resolved, &keys, 0)?;
                }
            }
            Body::Enm(_) => {}
        }
    }
    let names = allocate(pkg, &b.syms)?;
    decls.sort_by(|a, c| a.coord.cmp(&c.coord));
    Ok(Plan { names, decls, imps, raw: b.raw, any: b.any })
}

fn direct(raw: &BTreeMap<String, Raw>, key: &str) -> Vec<String> {
    let named = |t: &Ty| match t {
        Ty::Named(k) if raw.contains_key(k) => Some(k.clone()),
        _ => None,
    };
    match &raw[key].body {
        Body::Obj(fs) => fs.iter().filter_map(|f| named(&f.ty)).collect(),
        Body::Uni(vs) => vs.iter().filter_map(named).collect(),
        _ => vec![],
    }
}

fn reaches_type(raw: &BTreeMap<String, Raw>, from: &str, to: &str, seen: &mut HashSet<String>) -> bool {
    from == to || (seen.insert(from.into()) && direct(raw, from).iter().any(|k| reaches_type(raw, k, to, seen)))
}

pub fn boxed(raw: &BTreeMap<String, Raw>) -> HashSet<(String, String)> {
    let mut out = HashSet::new();
    for owner in raw.keys() {
        for target in direct(raw, owner) {
            if reaches_type(raw, &target, owner, &mut HashSet::new()) {
                out.insert((owner.clone(), target));
            }
        }
    }
    out
}

fn subst(t: &Ty, resolved: &HashMap<String, Ty>, keys: &HashSet<String>, depth: usize) -> R<Ty> {
    match t {
        Ty::Named(k) if !keys.contains(k) => match resolved.get(k) {
            Some(next) if depth < 64 => subst(next, resolved, keys, depth + 1),
            Some(_) => Err(e("RCR1012", k, "circular reference chain")),
            None => Err(e("RCR1012", k, "unresolved reference")),
        },
        _ => Ok(t.clone()),
    }
}
