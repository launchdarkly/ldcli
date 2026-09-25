//! Requests to the LaunchDarkly API.
//!
//! A port of `ResourcesClient.MakeRequest` in `internal/resources`: the access
//! token goes in `Authorization` as is, the user agent names the CLI version,
//! and an error status comes back as a JSON body the output layer renders.

use crate::output::gojson;
use serde_json::{Map, Value};
use std::io::Read;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RequestError {
    /// The server answered with an error status. The text is the JSON body
    /// Go builds for it.
    Api(String),
    /// The request never got an answer.
    Transport(String),
}

impl RequestError {
    pub fn into_cmd_error(self) -> crate::output::CmdError {
        match self {
            Self::Api(body) => crate::output::CmdError::ApiBody(body),
            Self::Transport(message) => crate::output::CmdError::Other(message),
        }
    }
}

pub struct Request<'a> {
    pub access_token: &'a str,
    pub method: &'a str,
    pub url: &'a str,
    pub content_type: &'a str,
    pub body: &'a [u8],
    pub beta: bool,
    pub version: &'a str,
}

pub fn user_agent(version: &str) -> String {
    format!("launchdarkly-cli/v{version}")
}

/// `url.JoinPath` for the simple case the CLI uses: a base and path segments.
pub fn join_path(base: &str, path: &str) -> String {
    format!(
        "{}/{}",
        base.trim_end_matches('/'),
        path.trim_start_matches('/')
    )
}

pub fn make_request(request: &Request<'_>) -> Result<Vec<u8>, RequestError> {
    let mut call = ureq::request(request.method, request.url)
        .set("Authorization", request.access_token)
        .set("Content-Type", request.content_type)
        .set("User-Agent", &user_agent(request.version));
    if request.beta {
        call = call.set("LD-API-Version", "beta");
    }
    let response = if request.body.is_empty() {
        call.call()
    } else {
        call.send_bytes(request.body)
    };
    match response {
        Ok(response) => read_body(response).map_err(RequestError::Transport),
        Err(ureq::Error::Status(status, response)) => {
            let body = read_body(response).unwrap_or_default();
            Err(RequestError::Api(error_body(
                status,
                &body,
                &scheme_and_host(request.url),
            )))
        }
        Err(ureq::Error::Transport(transport)) => {
            Err(RequestError::Transport(transport.to_string()))
        }
    }
}

fn read_body(response: ureq::Response) -> Result<Vec<u8>, String> {
    let mut body = Vec::new();
    response
        .into_reader()
        .read_to_end(&mut body)
        .map_err(|err| err.to_string())?;
    Ok(body)
}

/// The JSON Go builds for an error status. A JSON object body keeps its
/// fields and gains `statusCode`; anything else becomes the message. Common
/// statuses add a suggestion that names the base URI.
pub fn error_body(status: u16, body: &[u8], base_uri: &str) -> String {
    let text = String::from_utf8_lossy(body);
    let status_value = Value::from(status);
    let mut fields: Map<String, Value> = if body.is_empty() {
        Map::from_iter([
            ("code".to_string(), Value::String(status_code_name(status))),
            (
                "message".to_string(),
                Value::String(status_text(status).to_string()),
            ),
            ("statusCode".to_string(), status_value),
        ])
    } else {
        match gojson::decode(&text) {
            Ok(Value::Object(mut map)) => {
                map.entry("statusCode").or_insert(status_value);
                map
            }
            _ => Map::from_iter([
                ("code".to_string(), Value::String(status_code_name(status))),
                ("message".to_string(), Value::String(text.into_owned())),
                ("statusCode".to_string(), status_value),
            ]),
        }
    };
    if let Some(suggestion) = suggestion_for_status(status, base_uri) {
        fields.insert("suggestion".to_string(), Value::String(suggestion));
    }
    gojson::marshal(&Value::Object(fields))
}

fn scheme_and_host(url: &str) -> String {
    match url.split_once("://") {
        Some((scheme, rest)) => {
            let host = rest.split('/').next().unwrap_or("");
            format!("{scheme}://{host}")
        }
        None => String::new(),
    }
}

/// `errors.SuggestionForStatus`.
fn suggestion_for_status(status: u16, base_uri: &str) -> Option<String> {
    let text = match status {
        401 => "Your access token may be invalid or expired. Run `ldcli login` or set LD_ACCESS_TOKEN. Create a new token at {baseURI}/settings/authorization.",
        403 => "You don't have permission for this action. Check your access token's role and custom role policies in LaunchDarkly settings.",
        404 => "Resource not found. Verify the project key, flag key, or environment key. Use `ldcli projects list` or `ldcli flags list` to see available resources.",
        409 => "Conflict: the resource may have been modified since you last read it. Fetch the latest version and retry.",
        429 => "Rate limited. Wait a moment and retry. If this persists, check your account's rate limit allocation.",
        _ => return None,
    };
    Some(text.replace("{baseURI}", base_uri))
}

/// `http.StatusText` lowercased with underscores, as Go builds the code.
fn status_code_name(status: u16) -> String {
    status_text(status).to_lowercase().replace(' ', "_")
}

/// `http.StatusText` for the statuses an API can plausibly return.
fn status_text(status: u16) -> &'static str {
    match status {
        400 => "Bad Request",
        401 => "Unauthorized",
        402 => "Payment Required",
        403 => "Forbidden",
        404 => "Not Found",
        405 => "Method Not Allowed",
        406 => "Not Acceptable",
        408 => "Request Timeout",
        409 => "Conflict",
        410 => "Gone",
        411 => "Length Required",
        412 => "Precondition Failed",
        413 => "Request Entity Too Large",
        415 => "Unsupported Media Type",
        422 => "Unprocessable Entity",
        429 => "Too Many Requests",
        500 => "Internal Server Error",
        501 => "Not Implemented",
        502 => "Bad Gateway",
        503 => "Service Unavailable",
        504 => "Gateway Timeout",
        _ => "",
    }
}

/// `Service.VerifyAccessToken`: a HEAD request to the account endpoint. Any
/// transport failure or error status means the token is not usable.
pub fn verify_access_token(access_token: &str, base_uri: &str, version: &str) -> bool {
    make_request(&Request {
        access_token,
        method: "HEAD",
        url: &join_path(base_uri, "api/v2/account"),
        content_type: "application/json",
        body: &[],
        beta: false,
        version,
    })
    .is_ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use std::net::TcpListener;

    #[test]
    fn a_path_joins_onto_a_base_with_or_without_a_trailing_slash() {
        assert_eq!(
            join_path("https://app.launchdarkly.com", "api/v2/account"),
            "https://app.launchdarkly.com/api/v2/account"
        );
        assert_eq!(
            join_path("http://127.0.0.1:1/", "settings/authorization"),
            "http://127.0.0.1:1/settings/authorization"
        );
    }

    #[test]
    fn an_error_body_gains_a_status_code_and_a_suggestion() {
        assert_eq!(
            error_body(401, br#"{"code":"unauthorized","message":"bad"}"#, "http://h"),
            "{\"code\":\"unauthorized\",\"message\":\"bad\",\"statusCode\":401,\"suggestion\":\"Your access token may be invalid or expired. Run `ldcli login` or set LD_ACCESS_TOKEN. Create a new token at http://h/settings/authorization.\"}"
        );
        // A status code already in the body is kept as sent.
        assert_eq!(
            error_body(400, br#"{"statusCode":418}"#, "http://h"),
            "{\"statusCode\":418}"
        );
    }

    #[test]
    fn a_body_that_is_not_a_json_object_becomes_the_message() {
        assert_eq!(
            error_body(500, b"oops", "http://h"),
            "{\"code\":\"internal_server_error\",\"message\":\"oops\",\"statusCode\":500}"
        );
        assert_eq!(
            error_body(500, b"[1]", "http://h"),
            "{\"code\":\"internal_server_error\",\"message\":\"[1]\",\"statusCode\":500}"
        );
        assert_eq!(
            error_body(503, b"", "http://h"),
            "{\"code\":\"service_unavailable\",\"message\":\"Service Unavailable\",\"statusCode\":503}"
        );
    }

    fn serve_once(response: &'static str) -> (String, std::thread::JoinHandle<String>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let handle = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf = [0u8; 4096];
            let n = std::io::Read::read(&mut stream, &mut buf).unwrap();
            let request = String::from_utf8_lossy(&buf[..n]).into_owned();
            let _ = stream.write_all(response.as_bytes());
            request
        });
        (base, handle)
    }

    #[test]
    fn a_success_verifies_and_sends_the_go_headers() {
        let (base, handle) = serve_once("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n");
        assert!(verify_access_token("api-token", &base, "test"));
        let request = handle.join().unwrap().to_lowercase();
        assert!(request.starts_with("head /api/v2/account "), "{request}");
        assert!(request.contains("authorization: api-token"), "{request}");
        assert!(
            request.contains("content-type: application/json"),
            "{request}"
        );
        assert!(
            request.contains("user-agent: launchdarkly-cli/vtest"),
            "{request}"
        );
    }

    #[test]
    fn an_error_status_or_no_server_does_not_verify() {
        let (base, handle) = serve_once("HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\n\r\n");
        assert!(!verify_access_token("api-token", &base, "test"));
        handle.join().unwrap();
        assert!(!verify_access_token(
            "api-token",
            "http://127.0.0.1:1",
            "test"
        ));
    }
}
