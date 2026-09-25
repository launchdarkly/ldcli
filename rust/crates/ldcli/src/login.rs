//! `ldcli login`: the OAuth device flow, ending with the token in the config
//! file.
//!
//! A port of `cmd/login/login.go` and `internal/login`. The token is written
//! without being verified, and only a token already in the config file stops
//! a login; one from the environment or a flag does not.

use crate::cli::Outcome;
use crate::godecode::{self, Field, Kind, Struct};
use crate::http::{self, Request, RequestError};
use crate::{browser, config, gourl};
use std::ffi::OsString;
use std::path::Path;
use std::time::Duration;

pub const CLIENT_ID: &str = "e6506150369268abae3ed46152687201";
/// Two minutes at the one-second interval.
pub const MAX_FETCH_TOKEN_ATTEMPTS: usize = 120;
pub const TOKEN_INTERVAL: Duration = Duration::from_secs(1);

pub const ALREADY_SET: &str =
    "Your access token is already set. Remove it from the config if you wish to reset it.";

const DEVICE_AUTHORIZATION: Struct<'static> = Struct {
    name: "DeviceAuthorization",
    type_string: "login.DeviceAuthorization",
    fields: &[
        ("deviceCode", Kind::String),
        ("expiresIn", Kind::Int),
        ("userCode", Kind::String),
        ("verificationUri", Kind::String),
    ],
};

const DEVICE_AUTHORIZATION_TOKEN: Struct<'static> = Struct {
    name: "DeviceAuthorizationToken",
    type_string: "login.DeviceAuthorizationToken",
    fields: &[("accessToken", Kind::String)],
};

/// The anonymous struct `FetchToken` decodes an error into.
const TOKEN_ERROR: Struct<'static> = Struct {
    name: "",
    type_string: "struct { Code string; Message string }",
    fields: &[("code", Kind::String), ("message", Kind::String)],
};

pub struct LoginContext<'a> {
    pub config_path: &'a Path,
    pub base_uri: &'a str,
    pub version: &'a str,
    /// The `PATH` the browser opener is looked up on.
    pub path: Option<OsString>,
    pub device_name: String,
    pub interval: Duration,
    pub max_attempts: usize,
    /// Written as soon as the code is known, before polling starts.
    pub stdout: &'a dyn Fn(&str),
}

pub fn run(ctx: &LoginContext<'_>) -> Outcome {
    match login(ctx) {
        Ok(()) => Outcome::Stdout("Your token has been written to the configuration file\n".into()),
        Err(message) => Outcome::Failure(format!("{message}\n")),
    }
}

fn login(ctx: &LoginContext<'_>) -> Result<(), String> {
    let conf = config::load_strict(ctx.config_path)?;
    if conf.access_token.as_deref().is_some_and(|t| !t.is_empty()) {
        return Err(ALREADY_SET.to_string());
    }

    let authorization = fetch_device_authorization(ctx)?;
    let (device_code, user_code, verification_uri) = (
        authorization[0].as_str(),
        authorization[2].as_str(),
        authorization[3].as_str(),
    );
    // Go discards the join error, so an unparsable base URI prints an empty
    // URL and opens nothing useful.
    let full_url = gourl::join_path(ctx.base_uri, &[verification_uri]).unwrap_or_default();
    (ctx.stdout)(&format!(
        "Your code is {user_code}\n\
         This code verifies your authentication with LaunchDarkly.\n\
         Opening your browser to {full_url} to finish verifying your login.\n\
         If your browser does not open automatically, you can paste the above URL into your browser.\n"
    ));
    let _ = browser::open_url(&full_url, ctx.path.as_deref());

    let token = fetch_token(ctx, device_code)?;

    let conf = config::load_strict(ctx.config_path)?;
    let (updated, _) = conf.update(&["access-token".to_string(), token])?;
    config::write_atomic(ctx.config_path, &updated.to_yaml(None)).map_err(|err| err.to_string())
}

fn post(ctx: &LoginContext<'_>, url: &str, body: &[u8]) -> Result<Vec<u8>, RequestError> {
    http::make_request(&Request {
        access_token: "",
        method: "POST",
        url,
        content_type: "application/json",
        body,
        beta: false,
        version: ctx.version,
    })
}

fn error_text(err: RequestError) -> String {
    match err {
        RequestError::Api(body) | RequestError::Transport(body) => body,
    }
}

/// The request body is a Go raw string with `%q` values, so it keeps the
/// tabs of the source it was written in.
fn device_authorization_body(device_name: &str) -> String {
    format!(
        "{{\n\t\t\t\"clientId\": {},\n\t\t\t\"deviceName\": {}\n\t\t}}",
        crate::gostr::quote(CLIENT_ID),
        crate::gostr::quote(device_name)
    )
}

fn fetch_device_authorization(ctx: &LoginContext<'_>) -> Result<Vec<Field>, String> {
    // Plain concatenation: a base URI ending in a slash asks for `//internal`.
    let url = format!("{}/internal/device-authorization", ctx.base_uri);
    let body = post(
        ctx,
        &url,
        device_authorization_body(&ctx.device_name).as_bytes(),
    )
    .map_err(error_text)?;
    godecode::unmarshal(&body, &DEVICE_AUTHORIZATION)
}

/// `FetchToken`. Every failed request is read back as a JSON error with a
/// `code`, including failures that are not JSON at all, which is how a bad
/// success body becomes "error reading response". The attempt check runs
/// before each request and uses `>`, so a pending login gets one request
/// more than the maximum.
fn fetch_token(ctx: &LoginContext<'_>, device_code: &str) -> Result<String, String> {
    let url = format!("{}/internal/device-authorization/token", ctx.base_uri);
    let body = format!(
        "{{\"deviceCode\":{}}}",
        crate::output::gojson::marshal(&serde_json::Value::String(device_code.to_string()))
    );
    let mut attempts = 0;
    loop {
        if attempts > ctx.max_attempts {
            return Err("The request timed out after too many attempts.".to_string());
        }
        let failure = match post(ctx, &url, body.as_bytes()) {
            Ok(response) => match godecode::unmarshal(&response, &DEVICE_AUTHORIZATION_TOKEN) {
                Ok(fields) => return Ok(fields[0].as_str().to_string()),
                Err(message) => message,
            },
            Err(err) => error_text(err),
        };
        let fields = godecode::unmarshal(failure.as_bytes(), &TOKEN_ERROR)
            .map_err(|_| "error reading response".to_string())?;
        match fields[0].as_str() {
            "authorization_pending" => attempts += 1,
            "access_denied" => return Err("Your request has been denied.".to_string()),
            "expired_token" => {
                return Err("Your request has expired. Please try logging in again.".to_string())
            }
            _ => {
                return Err(format!(
                    "We cannot complete your request: {}",
                    fields[1].as_str()
                ))
            }
        }
        std::thread::sleep(ctx.interval);
    }
}

/// `os.Hostname`, or `unknown` when it fails.
pub fn device_name() -> String {
    let mut buf = [0u8; 256];
    // SAFETY: the buffer is valid for its length, and gethostname writes at
    // most that many bytes.
    let rc = unsafe { libc::gethostname(buf.as_mut_ptr().cast(), buf.len()) };
    if rc != 0 {
        return "unknown".to_string();
    }
    let end = buf.iter().position(|b| *b == 0).unwrap_or(buf.len());
    if end == 0 {
        return "unknown".to_string();
    }
    String::from_utf8_lossy(&buf[..end]).into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::RefCell;
    use std::io::{Read, Write};
    use std::net::TcpListener;

    /// Answers each request with the next response, repeating the last, and
    /// returns the request lines it saw.
    fn serve(
        responses: Vec<&'static str>,
        count: usize,
    ) -> (String, std::thread::JoinHandle<Vec<String>>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let handle = std::thread::spawn(move || {
            let mut seen = Vec::new();
            for i in 0..count {
                let (mut stream, _) = listener.accept().unwrap();
                let request = read_request(&mut stream);
                seen.push(request.lines().next().unwrap_or("").to_string());
                let body = responses[i.min(responses.len() - 1)];
                let (status, body) = body.split_once(' ').unwrap();
                let _ = write!(
                    stream,
                    "HTTP/1.1 {status} X\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                    body.len()
                );
            }
            seen
        });
        (base, handle)
    }

    /// Reading the whole body before answering keeps the client from seeing
    /// a reset while it is still sending.
    fn read_request(stream: &mut std::net::TcpStream) -> String {
        let mut data = Vec::new();
        let mut buf = [0u8; 4096];
        loop {
            let n = stream.read(&mut buf).unwrap();
            data.extend_from_slice(&buf[..n]);
            let text = String::from_utf8_lossy(&data).into_owned();
            if let Some(end) = text.find("\r\n\r\n") {
                let length = text[..end]
                    .lines()
                    .find_map(|line| {
                        let (name, value) = line.split_once(':')?;
                        name.eq_ignore_ascii_case("content-length")
                            .then(|| value.trim().parse::<usize>().ok())?
                    })
                    .unwrap_or(0);
                if data.len() >= end + 4 + length || n == 0 {
                    return text;
                }
            }
            if n == 0 {
                return text;
            }
        }
    }

    const AUTHORIZATION: &str =
        r#"200 {"deviceCode":"dc","userCode":"uc","verificationUri":"/confirm-auth/v"}"#;

    fn context<'a>(
        path: &'a Path,
        base: &'a str,
        max_attempts: usize,
        stdout: &'a dyn Fn(&str),
    ) -> LoginContext<'a> {
        LoginContext {
            config_path: path,
            base_uri: base,
            version: "test",
            path: Some(OsString::new()),
            device_name: "box".to_string(),
            interval: Duration::from_millis(1),
            max_attempts,
            stdout,
        }
    }

    fn config_file(contents: &str) -> (tempfile::TempDir, std::path::PathBuf) {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("config.yml");
        std::fs::write(&path, contents).unwrap();
        (dir, path)
    }

    #[test]
    fn a_granted_login_prints_the_code_first_and_stores_the_token() {
        let (_dir, path) = config_file("project: kept\n");
        let (base, server) = serve(vec![AUTHORIZATION, r#"200 {"accessToken":"tok"}"#], 2);
        let printed = RefCell::new(String::new());
        let stdout = |text: &str| printed.borrow_mut().push_str(text);
        let outcome = run(&context(&path, &base, 5, &stdout));
        assert_eq!(
            outcome,
            Outcome::Stdout("Your token has been written to the configuration file\n".into())
        );
        assert!(printed
            .borrow()
            .starts_with(&format!("Your code is uc\nThis code verifies your authentication with LaunchDarkly.\nOpening your browser to {base}/confirm-auth/v to finish")));
        assert_eq!(
            std::fs::read_to_string(&path).unwrap(),
            "access-token: tok\nproject: kept\n"
        );
        assert_eq!(
            server.join().unwrap(),
            vec![
                "POST /internal/device-authorization HTTP/1.1",
                "POST /internal/device-authorization/token HTTP/1.1"
            ]
        );
    }

    #[test]
    fn a_pending_login_polls_one_more_time_than_the_maximum() {
        let (_dir, path) = config_file("{}\n");
        let pending = r#"400 {"code":"authorization_pending"}"#;
        let (base, server) = serve(vec![AUTHORIZATION, pending], 4);
        let outcome = run(&context(&path, &base, 2, &|_| {}));
        assert_eq!(
            outcome,
            Outcome::Failure("The request timed out after too many attempts.\n".into())
        );
        assert_eq!(server.join().unwrap().len(), 4);
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "{}\n");
    }

    #[test]
    fn a_token_already_in_the_file_stops_the_login_before_any_request() {
        let (_dir, path) = config_file("access-token: existing\n");
        let outcome = run(&context(&path, "http://127.0.0.1:1", 1, &|_| {}));
        assert_eq!(outcome, Outcome::Failure(format!("{ALREADY_SET}\n")));
    }

    #[test]
    fn an_unknown_code_reports_the_server_message() {
        let (_dir, path) = config_file("{}\n");
        let (base, server) = serve(
            vec![
                AUTHORIZATION,
                r#"400 {"code":"slow_down","message":"easy"}"#,
            ],
            2,
        );
        assert_eq!(
            run(&context(&path, &base, 5, &|_| {})),
            Outcome::Failure("We cannot complete your request: easy\n".into())
        );
        server.join().unwrap();
    }

    #[test]
    fn the_device_authorization_body_keeps_go_source_indentation() {
        assert_eq!(
            device_authorization_body("my \"box\""),
            "{\n\t\t\t\"clientId\": \"e6506150369268abae3ed46152687201\",\n\t\t\t\"deviceName\": \"my \\\"box\\\"\"\n\t\t}"
        );
    }

    #[test]
    fn the_device_name_is_the_host_name() {
        assert!(!device_name().is_empty());
    }
}
