use serde_json::{json, Value};
use std::collections::{HashMap, HashSet};
use crate::common::*;
use crate::diag::*;
use crate::plan::*;
use crate::render::*;
use crate::yaml::*;

pub fn manifest(m: &Value, t: &Value, p: &Plan, h: &str) -> R<String> {
    let n = &p.names;
    let root = s(&m["rootExtension"], "id");
    let r = |x: &Ty| match x {
        Ty::Prim(b) => json!({"builtin": b}),
        Ty::Named(k) => json!({"type": n[k]}),
    };
    let contracts: HashMap<&str, &str> = p.decls.iter().map(|d| (d.method.as_str(), d.coord.as_str())).chain(p.imps.iter().map(|i| (i.method.as_str(), i.coord.as_str()))).collect();
    let mut types = vec![];
    for (k, raw) in &p.raw {
        let wrapper = matches!(raw.body, Body::Obj(_)) && s(&raw.shape, "kind") != "object";
        let construct = match &raw.body {
            Body::Obj(_) if wrapper => "wrapper",
            Body::Obj(_) => "object",
            Body::Enm(_) => "scalar",
            Body::Arr(_) => "collection",
            Body::Map(_) => "map",
            Body::Uni(_) => "union",
        };
        let prov = &raw.shape["provenance"];
        let mut v = json!({"modelRef": mref(prov), "sourceName": raw.src, "nativeName": n[k], "construct": construct, "file": format!("bindings/{}.rs", module_file(&n[k]))});
        match &raw.body {
            Body::Enm(ms) => {
                v["underlying"] = "string".into();
                v["members"] = ms.iter().map(|(mk, val)| json!({"modelRef": mref(prov), "nativeName": n[mk], "value": val})).collect();
            }
            Body::Obj(fs) => {
                let mut fs: Vec<&Fld> = fs.iter().collect();
                fs.sort_by(|a, b| (!a.req, &n[&a.key]).cmp(&(!b.req, &n[&b.key])));
                v["fields"] = fs
                    .iter()
                    .map(|f| {
                        let mut x = json!({"modelRef": mref(&f.prov), "sourceName": if wrapper { &raw.src } else { &f.src }, "nativeName": n[&f.key], "required": f.req, "value": r(&f.ty)});
                        if wrapper {
                            x["synthetic"] = true.into();
                        }
                        x
                    })
                    .collect();
                if let Some(mk) = &raw.mk {
                    v["implements"] = json!([{"declarationCoordinate": contracts[mk.method.as_str()], "markerMethod": mk.method}]);
                }
            }
            Body::Arr(x) | Body::Map(x) => {
                let c = &raw.shape[if construct == "collection" { "items" } else { "mapValues" }];
                v["element"] = json!({"modelRef": mref(&c["provenance"]), "value": r(x)});
            }
            Body::Uni(xs) => {
                v["variants"] = arr(&raw.shape, "variants").iter().zip(xs).map(|(c, x)| json!({"modelRef": mref(&c["provenance"]), "value": r(x)})).collect();
            }
        }
        types.push(v);
    }
    types.sort_by_key(|v| s(v, "nativeName").to_string());

    let decl: HashMap<&str, &Value> = arr(&m["vocabulary"], "ownedDeclarations")
        .iter()
        .chain(arr(&m["vocabulary"], "importedDeclarations"))
        .map(|d| (s(d, "kind"), d))
        .collect();
    let cfields: HashSet<(&str, &str, &str)> = arr(&m["vocabulary"], "conditionFields")
        .iter()
        .filter(|f| s(f, "owner") == root)
        .map(|f| (s(f, "kind"), s(f, "interfaceType"), s(f, "path")))
        .collect();
    let mut binds = vec![];
    for sc in arr(m, "schemas").iter().filter(|x| s(x, "owner") == root) {
        let kind = sc["kind"].as_str().unwrap_or_else(|| s(sc, "id"));
        let Some(d) = decl.get(kind) else { continue };
        let iface = s(sc, "interfaceType");
        for pr in arr(&sc["projection"], "properties") {
            let nm = s(pr, "name");
            let sco = s(sc, "coordinate");
            let native = n.get(&format!("type:field:{sco}:{nm}")).ok_or_else(|| e("RCR1012", sco, format!("root property '{nm}' has no declaration field type")))?;
            let is_if = nm == "interface" && !iface.is_empty();
            let vf = cfields.contains(&(kind, iface, nm));
            let role = if is_if { "interface" } else if vf { "condition-field" } else { "schema-field" };
            let mut scope = json!({"kind": kind});
            if !iface.is_empty() {
                scope["interfaceType"] = iface.into();
            }
            let mut b = json!({"role": role, "modelRef": mref(&pr["provenance"]), "declarationCoordinate": s(d, "coordinate"), "scope": scope, "sourceName": if is_if { iface } else { nm }, "path": [{"name": nm}], "value": {"type": native}, "schemaCoordinate": sco});
            if is_if {
                b["fixedInterfaceType"] = iface.into();
            }
            if vf {
                if let Some(f) = arr(&m["vocabulary"], "conditionFields").iter().find(|f| s(f, "owner") == root && s(f, "kind") == kind && s(f, "interfaceType") == iface && s(f, "path") == nm) {
                    b["vocabularyCoordinate"] = s(f, "coordinate").into();
                }
            }
            binds.push(b);
        }
    }
    binds.sort_by_key(|b| (s(b, "declarationCoordinate").to_string(), s(&b["modelRef"], "coordinate").to_string(), s(&b["modelRef"], "jsonPointer").to_string()));

    let crate_name = s(t, "crateName");
    let doc = json!({
        "apiVersion": "runtimeconditions.io/bindings/v1alpha2",
        "kind": "RuntimeConditionsBindingManifest",
        "generated": {"nonEditable": true, "emitter": format!("{NAME}@sha256:{}", s(t, "emitterSha256")), "version": VERSION},
        "model": {"apiVersion": s(m, "apiVersion"), "semanticSha256": s(&m["metadata"], "semanticSha256")},
        "extension": {"id": root, "semanticSha256": s(&m["rootExtension"], "semanticSha256")},
        "package": {"language": "rust", "coordinate": crate_name, "name": crate_name.replace('-', "_"), "version": s(t, "version"), "minimumRustVersion": s(t, "minimumRustVersion")},
        "declarations": p.decls.iter().map(|d| json!({"modelRef": mref(&d.prov), "owner": root, "sourceName": d.kind, "function": n[&d.func], "markerInterface": n[&d.proto], "markerMethod": d.method, "file": "bindings.rs"})).collect::<Vec<_>>(),
        "importedMarkerContracts": p.imps.iter().map(|i| json!({"modelRef": mref(&i.prov), "owner": i.owner, "sourceName": i.kind, "markerInterface": n[&i.proto], "markerMethod": i.method, "providerPackage": i.krate})).collect::<Vec<_>>(),
        "rootBindings": binds,
        "types": types,
    });
    let mut out = format!("# {h}\n");
    yaml(&doc, 0, false, &mut out);
    Ok(out)
}
