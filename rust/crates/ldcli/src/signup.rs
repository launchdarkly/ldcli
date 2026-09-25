//! `ldcli signup`: open the signup page in a browser.
//!
//! A port of `cmd/signup/signup.go`. A browser that fails to open is only a
//! warning; the command still succeeds.

use crate::cli::Outcome;
use crate::{browser, gourl};
use std::ffi::OsString;

pub struct SignupContext<'a> {
    pub base_uri: &'a str,
    /// The `PATH` the browser opener is looked up on.
    pub path: Option<OsString>,
    /// Written before the browser starts, since the opener shares stdout.
    pub stdout: &'a dyn Fn(&str),
}

pub fn run(ctx: &SignupContext<'_>) -> Outcome {
    let url = match gourl::join_path(ctx.base_uri, &["/signup"]) {
        Ok(url) => url,
        Err(err) => return Outcome::Failure(format!("failed to construct signup URL: {err}\n")),
    };
    (ctx.stdout)(&format!(
        "Opening your browser to {url} to create a new LaunchDarkly account.\n\
         If your browser does not open automatically, you can paste the above URL into your browser.\n"
    ));
    match browser::open_url(&url, ctx.path.as_deref()) {
        Ok(()) => Outcome::Stdout(String::new()),
        Err(err) => Outcome::StderrOk(format!(
            "Warning: failed to open browser automatically: {err}\n"
        )),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::RefCell;

    fn signup(base_uri: &str) -> (Outcome, String) {
        let printed = RefCell::new(String::new());
        let stdout = |text: &str| printed.borrow_mut().push_str(text);
        let outcome = run(&SignupContext {
            base_uri,
            path: Some(OsString::new()),
            stdout: &stdout,
        });
        (outcome, printed.into_inner())
    }

    #[test]
    fn the_signup_page_hangs_off_the_base_path() {
        let (_, printed) = signup("http://example.test/x/");
        assert!(
            printed.starts_with(
                "Opening your browser to http://example.test/x/signup to create a new LaunchDarkly account.\n"
            ),
            "{printed}"
        );
    }

    #[test]
    fn a_browser_that_cannot_open_is_a_warning_and_still_succeeds() {
        let (outcome, _) = signup("https://app.launchdarkly.com");
        match outcome {
            Outcome::StderrOk(text) => assert!(
                text.starts_with("Warning: failed to open browser automatically: exec: "),
                "{text}"
            ),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn an_unparsable_base_uri_fails_before_printing_anything() {
        let (outcome, printed) = signup("http://example.test:abc");
        assert_eq!(
            outcome,
            Outcome::Failure(
                "failed to construct signup URL: parse \"http://example.test:abc\": invalid port \":abc\" after host\n"
                    .into()
            )
        );
        assert_eq!(printed, "");
    }
}
