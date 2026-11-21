//! 错误码：码值由 docs/spec/error-codes.md 生成（scripts/gen-errors.mjs）。
//! 本文件只保留类型与工具函数，常量见 errors_gen.rs。
//! 校验命令：node scripts/gen-errors.mjs --check

include!("errors_gen.rs");

/// 码段（1..9），用于校验码值划分规则。
pub fn segment(code: RawCode) -> u32 {
    code / 100_000
}

/// 结构化错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ErError {
    pub code: RawCode,
    pub detail: String,
}

impl ErError {
    pub fn new(code: RawCode, detail: impl Into<String>) -> Self {
        Self { code, detail: detail.into() }
    }

    pub fn name(&self) -> &'static str {
        name(self.code)
    }

    pub fn is(&self, code: RawCode) -> bool {
        self.code == code
    }

    pub fn segment(&self) -> u32 {
        segment(self.code)
    }
}

impl std::fmt::Display for ErError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{} {}: {}", self.code, self.name(), self.detail)
    }
}

impl std::error::Error for ErError {}

/// 把 ErError 转成可序列化的结构，供跨语言接口使用。
impl ErError {
    pub fn to_wire(&self) -> (RawCode, &'static str, &str) {
        (self.code, self.name(), self.detail.as_str())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn codes_are_unique_and_ascending() {
        let list = all();
        assert!(!list.is_empty(), "no codes generated");
        let mut prev = 0;
        for (code, name) in list {
            assert!(*code > prev, "codes must ascend: {} after {}", code, prev);
            assert!(!name.is_empty());
            prev = *code;
        }
    }

    #[test]
    fn names_are_unique() {
        let mut seen = std::collections::HashSet::new();
        for (_, n) in all() {
            assert!(seen.insert(*n), "duplicate name {}", n);
        }
    }

    #[test]
    fn segments_are_valid() {
        for (code, n) in all() {
            let seg = segment(*code);
            assert!((1..=9).contains(&seg), "{} {} bad segment", code, n);
            assert_ne!(code % 100_000, 0, "{} reserved", code);
        }
    }

    #[test]
    fn unknown_code_is_reported() {
        assert_eq!(name(999_999), "UNKNOWN");
    }

    #[test]
    fn error_display_and_predicates() {
        let e = ErError::new(CONN_CAPTCHA_LOCKED, "locked");
        assert!(e.to_string().contains("CONN_CAPTCHA_LOCKED"));
        assert!(e.is(CONN_CAPTCHA_LOCKED));
        assert!(!e.is(CONN_CAPTCHA_INVALID));
        assert_eq!(e.segment(), 2);
    }
}
