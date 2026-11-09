pub type R<T> = Result<T, Diag>;

pub const SEP: char = '\u{1f}';

pub struct Diag {
    pub category: &'static str,
    pub code: String,
    pub coordinate: String,
    pub pointer: String,
    pub message: String,
}

impl std::fmt::Display for Diag {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        let place = if self.pointer.is_empty() { self.coordinate.clone() } else { format!("{} {}", self.coordinate, self.pointer) };
        write!(f, "{} {place}: {}", self.code, self.message)
    }
}

pub fn ep(code: &str, at: &str, pointer: &str, msg: impl std::fmt::Display) -> Diag {
    let (coordinate, joined) = at.split_once(SEP).unwrap_or((at, ""));
    let category = match code.as_bytes().get(3) {
        Some(b'0') => "usage",
        Some(b'1') if code == "RCR1001" || code == "RCR1002" => "input",
        Some(b'1') if ("RCR1006"..="RCR1009").contains(&code) => "package-config",
        Some(b'1') => "model",
        Some(b'2') => "rust-naming",
        Some(b'3') => "output",
        _ => "verification",
    };
    Diag { category, code: code.into(), coordinate: coordinate.into(), pointer: if pointer.is_empty() { joined.into() } else { pointer.into() }, message: msg.to_string() }
}

pub fn e(code: &str, at: &str, msg: impl std::fmt::Display) -> Diag {
    ep(code, at, "", msg)
}

pub fn shown(at: &str) -> String {
    at.replace(SEP, "")
}
