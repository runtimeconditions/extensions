use serde_json::Value;

pub fn scalar(x: &str) -> String {
    let plain = !x.is_empty()
        && !x.starts_with(|c: char| "-?:,[]{}#&*!|>'\"%@` ".contains(c))
        && !x.ends_with([' ', ':'])
        && !x.contains(": ")
        && !x.contains(" #")
        && x.chars().all(|c| c.is_alphanumeric() || "_-./:@#+=,()$ <>*{}[]".contains(c))
        && x.parse::<f64>().is_err()
        && !["true", "false", "null", "yes", "no", "on", "off", "~"].contains(&x.to_lowercase().as_str());
    if plain { x.into() } else { format!("'{}'", x.replace('\'', "''")) }
}

pub fn inline(v: &Value) -> String {
    match v {
        Value::String(x) => scalar(x),
        Value::Array(_) => "[]".into(),
        Value::Object(_) => "{}".into(),
        x => x.to_string(),
    }
}

pub fn yaml(v: &Value, ind: usize, first_inline: bool, o: &mut String) {
    let pad = " ".repeat(ind);
    match v {
        Value::Object(m) => {
            for (i, (k, x)) in m.iter().enumerate() {
                if !(first_inline && i == 0) {
                    o.push_str(&pad);
                }
                o.push_str(&format!("{}:", scalar(k)));
                match x {
                    Value::Object(c) if !c.is_empty() => {
                        o.push('\n');
                        yaml(x, ind + 2, false, o);
                    }
                    Value::Array(a) if !a.is_empty() => {
                        o.push('\n');
                        yaml(x, ind + 2, false, o);
                    }
                    _ => o.push_str(&format!(" {}\n", inline(x))),
                }
            }
        }
        Value::Array(a) => {
            for x in a {
                o.push_str(&format!("{pad}- "));
                match x {
                    Value::Object(c) if !c.is_empty() => yaml(x, ind + 2, true, o),
                    _ => o.push_str(&format!("{}\n", inline(x))),
                }
            }
        }
        _ => {}
    }
}
