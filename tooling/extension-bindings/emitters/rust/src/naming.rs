use unicode_normalization::UnicodeNormalization;
use std::collections::{BTreeMap, HashMap};
use crate::diag::*;

pub const KEYWORDS: &[&str] = &[
    "_", "abstract", "as", "async", "await", "become", "box", "break", "const", "continue", "crate", "do", "dyn",
    "else", "enum", "extern", "false", "final", "fn", "for", "gen", "if", "impl", "in", "let", "loop", "macro",
    "match", "mod", "move", "mut", "override", "priv", "pub", "ref", "return", "self", "Self", "static", "struct",
    "super", "trait", "true", "try", "type", "typeof", "unsafe", "unsized", "use", "virtual", "where", "while",
    "yield",
];

pub const NOT_RAW: &[&str] = &["_", "self", "Self", "super", "crate"];

pub const RESERVED: &[&str] = &[
    "Option", "Some", "None", "Result", "Ok", "Err", "Vec", "String", "Box", "BTreeMap", "Value", "Clone", "Copy",
    "Debug", "PartialEq", "Eq", "Default", "Send", "Sync", "Sized", "Drop", "Fn", "From", "Into", "Iterator",
];

#[derive(Clone, Copy, PartialEq)]
pub enum St {
    Pascal,
    Snake,
    Exact,
}

pub fn ident(n: &str) -> bool {
    let mut c = n.chars();
    c.next().is_some_and(|f| f.is_alphabetic() || f == '_') && c.all(|x| x.is_alphanumeric() || x == '_') && n != "_"
}

pub fn tokens(x: &str) -> Vec<String> {
    let c: Vec<char> = x.nfkc().collect();
    let (mut out, mut cur) = (vec![], String::new());
    for i in 0..c.len() {
        if !c[i].is_alphanumeric() {
            if !cur.is_empty() {
                out.push(std::mem::take(&mut cur));
            }
            continue;
        }
        let p = i.checked_sub(1).map(|j| c[j]);
        let n = c.get(i + 1).copied();
        let cut = c[i].is_uppercase()
            && p.is_some_and(|p| {
                p.is_lowercase() || p.is_ascii_digit() || (p.is_uppercase() && n.is_some_and(|n| n.is_lowercase()))
            });
        if cut && !cur.is_empty() {
            out.push(std::mem::take(&mut cur));
        }
        cur.push(c[i]);
    }
    if !cur.is_empty() {
        out.push(cur);
    }
    out
}

pub fn name(src: &str, st: St, at: &str) -> R<String> {
    let bad = |n: &str| e("RCR2000", at, format!("invalid Rust identifier {n:?}"));
    if st == St::Exact {
        return if ident(src) { Ok(src.into()) } else { Err(bad(src)) };
    }
    let t = tokens(src);
    if t.is_empty() {
        return Err(bad(src));
    }
    let mut n = if st == St::Pascal {
        t.iter()
            .map(|p| {
                let mut c = p.chars();
                c.next().map(|f| f.to_uppercase().chain(c.flat_map(char::to_lowercase)).collect::<String>()).unwrap()
            })
            .collect()
    } else {
        t.iter().map(|p| p.to_lowercase()).collect::<Vec<_>>().join("_")
    };
    if n.starts_with(|c: char| c.is_numeric()) {
        n.insert(0, if st == St::Snake { 'x' } else { 'X' });
    }
    if !ident(&n) {
        return Err(bad(&n));
    }
    if KEYWORDS.contains(&n.as_str()) {
        if NOT_RAW.contains(&n.as_str()) { n.push('_') } else { n.insert_str(0, "r#") }
    }
    Ok(n)
}

pub struct Sym {
    pub key: String,
    pub at: String,
    pub src: String,
    pub st: St,
    pub ns: String,
    pub par: Vec<String>,
    pub fixed: bool,
}

pub fn cand(y: &Sym, l: usize) -> R<String> {
    let mut p: Vec<&str> = y.par[..l].iter().rev().map(String::as_str).collect();
    p.push(&y.src);
    name(&p.join("_"), y.st, &y.at)
}

pub fn allocate(pkg: &str, syms: &[Sym]) -> R<HashMap<String, String>> {
    let mut lv: HashMap<&str, usize> = syms.iter().map(|y| (y.key.as_str(), 0)).collect();
    if lv.len() != syms.len() {
        return Err(e("RCR2001", pkg, "duplicate symbol request key"));
    }
    let depth = syms.iter().map(|y| y.par.len()).max().unwrap_or(0);
    for _ in 0..=syms.len() * (depth + 1) {
        let mut groups: BTreeMap<(&str, String), Vec<&Sym>> = BTreeMap::new();
        for y in syms {
            groups.entry((&y.ns, cand(y, lv[y.key.as_str()])?)).or_default().push(y);
        }
        let mut hit = vec![];
        for ((ns, n), g) in &groups {
            let reserved = *ns == "module" && RESERVED.contains(&n.as_str());
            if g.len() > 1 || reserved {
                if reserved || g.iter().any(|y| y.fixed) {
                    let mut at: Vec<String> = g.iter().map(|y| shown(&y.at)).collect();
                    at.sort();
                    return Err(e("RCR2001", pkg, format!("symbol '{n}' conflicts: {}", at.join(" and "))));
                }
                hit.push(g.clone());
            }
        }
        if hit.is_empty() {
            return syms.iter().map(|y| Ok((y.key.clone(), cand(y, lv[y.key.as_str()])?))).collect();
        }
        for g in hit {
            for y in &g {
                let l = lv.get_mut(y.key.as_str()).unwrap();
                if *l == y.par.len() {
                    let mut at: Vec<String> = g.iter().map(|y| shown(&y.at)).collect();
                    at.sort();
                    return Err(e("RCR2001", pkg, format!("symbol '{}' conflicts after full path: {}", y.src, at.join(" and "))));
                }
                *l += 1;
            }
        }
    }
    Err(e("RCR2001", pkg, "symbol allocation did not converge"))
}
