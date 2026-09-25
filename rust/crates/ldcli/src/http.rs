//! Requests to the LaunchDarkly API.
//!
//! Headers follow `ResourcesClient.MakeRequest` in `internal/resources`: the
//! access token goes in `Authorization` as is, and the user agent names the
//! CLI version.

pub fn user_agent(version: &str) -> String {
    format!("launchdarkly-cli/v{version}")
}

/// `url.JoinPath` for the simple case the CLI uses: one base, one relative path.
pub fn join_path(base: &str, path: &str) -> String {
    format!(
        "{}/{}",
        base.trim_end_matches('/'),
        path.trim_start_matches('/')
    )
}

/// `Service.VerifyAccessToken`: a HEAD request to the account endpoint. Any
/// transport failure or error status means the token is not usable.
pub fn verify_access_token(access_token: &str, base_uri: &str, version: &str) -> bool {
    ureq::head(&join_path(base_uri, "api/v2/account"))
        .set("Authorization", access_token)
        .set("Content-Type", "application/json")
        .set("User-Agent", &user_agent(version))
        .call()
        .is_ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};
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

    /// Serve one response and hand back the request that arrived.
    fn serve_once(status: &str) -> (String, std::thread::JoinHandle<String>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let status = status.to_string();
        let handle = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf = [0u8; 4096];
            let n = stream.read(&mut buf).unwrap();
            let request = String::from_utf8_lossy(&buf[..n]).into_owned();
            let _ = write!(stream, "HTTP/1.1 {status}\r\nContent-Length: 0\r\n\r\n");
            request
        });
        (base, handle)
    }

    #[test]
    fn a_success_status_verifies_and_sends_the_go_headers() {
        let (base, handle) = serve_once("204 No Content");
        assert!(verify_access_token("api-token", &base, "test"));
        let request = handle.join().unwrap().to_lowercase();
        assert!(request.starts_with("head /api/v2/account "), "{request}");
        assert!(request.contains("authorization: api-token"), "{request}");
        assert!(
            request.contains("user-agent: launchdarkly-cli/vtest"),
            "{request}"
        );
    }

    #[test]
    fn an_error_status_or_no_server_does_not_verify() {
        let (base, handle) = serve_once("401 Unauthorized");
        assert!(!verify_access_token("api-token", &base, "test"));
        handle.join().unwrap();
        assert!(!verify_access_token(
            "api-token",
            "http://127.0.0.1:1",
            "test"
        ));
    }
}
