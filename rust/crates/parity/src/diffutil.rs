//! Unified diffs of stdout, stderr, and declared files.

use similar::TextDiff;

pub fn unified_diff(
    left_label: &str,
    right_label: &str,
    left: &str,
    right: &str,
) -> Option<String> {
    if left == right {
        return None;
    }
    let diff = TextDiff::from_lines(left, right);
    let text = diff
        .unified_diff()
        .context_radius(3)
        .header(left_label, right_label)
        .to_string();
    Some(text)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn one_changed_flag_line_is_visible() {
        let left = "Flags:\n      --alpha   first\n      --keep    same\n";
        let right = "Flags:\n      --beta    first\n      --keep    same\n";
        let diff = unified_diff("go", "rust", left, right).expect("diff");
        assert!(diff.contains("--alpha"), "{diff}");
        assert!(diff.contains("--beta"), "{diff}");
        assert!(diff.contains("--- go"), "{diff}");
        assert!(diff.contains("+++ rust"), "{diff}");
    }

    #[test]
    fn identical_text_has_no_diff() {
        assert!(unified_diff("go", "rust", "same\n", "same\n").is_none());
    }
}
