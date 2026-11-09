use std::collections::BTreeMap;
use std::{fs, path::Path};
use crate::diag::*;

pub fn api(files: &BTreeMap<String, String>) -> R<Vec<String>> {
    use syn::{Item, Visibility};
    use quote::ToTokens;
    let mut out = vec![];
    for (file, src) in files.iter().filter(|(f, _)| f.starts_with("src/") && f.ends_with(".rs")) {
        for i in syn::parse_file(src).map_err(|x| e("RCR4001", file, x))?.items {
            out.push(match &i {
                Item::Fn(f) if matches!(f.vis, Visibility::Public(_)) => f.sig.to_token_stream().to_string(),
                Item::Impl(x) => format!("impl {} for {}", x.trait_.as_ref().map_or(String::new(), |t| t.1.to_token_stream().to_string()), x.self_ty.to_token_stream()),
                Item::Struct(syn::ItemStruct { vis: Visibility::Public(_), .. }) | Item::Enum(syn::ItemEnum { vis: Visibility::Public(_), .. }) | Item::Trait(syn::ItemTrait { vis: Visibility::Public(_), .. }) | Item::Use(_) | Item::Mod(_) => i.to_token_stream().to_string(),
                _ => return Err(e("RCR4001", file, "unexpected non-public item")),
            });
        }
    }
    Ok(out)
}

pub fn read_tree(dir: &Path, base: &Path, out: &mut BTreeMap<String, String>) -> R<()> {
    for ent in fs::read_dir(dir).map_err(|x| e("RCR4001", &dir.display().to_string(), x))? {
        let p = ent.map_err(|x| e("RCR4001", "input", x))?.path();
        if p.is_dir() {
            read_tree(&p, base, out)?;
        } else {
            let rel = p.strip_prefix(base).unwrap().to_string_lossy().into_owned();
            out.insert(rel, fs::read_to_string(&p).map_err(|x| e("RCR4001", &p.display().to_string(), x))?);
        }
    }
    Ok(())
}
