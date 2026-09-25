//! A local HTTP server that both binaries talk to instead of LaunchDarkly.
//!
//! Each run gets its own server with the case's canned routes. Every request
//! that arrives is recorded, so a case compares what each binary sent as well
//! as what it printed. A request with no matching route gets a 404 with an
//! empty body.

use anyhow::{anyhow, Result};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::Duration;

/// The headers a recording keeps. The rest vary by client library.
const RECORDED_HEADERS: [&str; 4] = [
    "authorization",
    "content-type",
    "ld-api-version",
    "user-agent",
];

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Route {
    pub method: String,
    pub path: String,
    #[serde(default = "ok_status")]
    pub status: u16,
    #[serde(default)]
    pub body: String,
}

fn ok_status() -> u16 {
    200
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Recorded {
    pub method: String,
    pub target: String,
    pub headers: BTreeMap<String, String>,
    pub body: String,
}

impl Recorded {
    fn render(&self) -> String {
        let mut out = format!("{} {}\n", self.method, self.target);
        for (name, value) in &self.headers {
            out.push_str(&format!("{name}: {value}\n"));
        }
        if !self.body.is_empty() {
            out.push('\n');
            out.push_str(&self.body);
            if !self.body.ends_with('\n') {
                out.push('\n');
            }
        }
        out
    }
}

/// All requests as one readable block, in arrival order.
pub fn render_requests(requests: &[Recorded]) -> String {
    requests
        .iter()
        .map(Recorded::render)
        .collect::<Vec<_>>()
        .join("---\n")
}

pub struct FixtureServer {
    pub base_uri: String,
    stop: Arc<AtomicBool>,
    handle: JoinHandle<Vec<Recorded>>,
}

impl FixtureServer {
    pub fn start(routes: Vec<Route>) -> Result<Self> {
        let listener =
            TcpListener::bind("127.0.0.1:0").map_err(|err| anyhow!("fixture bind: {err}"))?;
        listener
            .set_nonblocking(true)
            .map_err(|err| anyhow!("fixture nonblocking: {err}"))?;
        let base_uri = format!(
            "http://{}",
            listener.local_addr().map_err(|err| anyhow!(err))?
        );
        let stop = Arc::new(AtomicBool::new(false));
        let stop_flag = Arc::clone(&stop);
        let handle = thread::spawn(move || {
            let mut recorded = Vec::new();
            while !stop_flag.load(Ordering::SeqCst) {
                match listener.accept() {
                    Ok((stream, _)) => {
                        if let Some(request) = serve(stream, &routes) {
                            recorded.push(request);
                        }
                    }
                    Err(err) if err.kind() == std::io::ErrorKind::WouldBlock => {
                        thread::sleep(Duration::from_millis(5));
                    }
                    Err(_) => break,
                }
            }
            recorded
        });
        Ok(Self {
            base_uri,
            stop,
            handle,
        })
    }

    /// Stop serving and return what arrived.
    pub fn finish(self) -> Vec<Recorded> {
        self.stop.store(true, Ordering::SeqCst);
        self.handle.join().unwrap_or_default()
    }
}

fn serve(stream: TcpStream, routes: &[Route]) -> Option<Recorded> {
    stream.set_nonblocking(false).ok()?;
    stream
        .set_read_timeout(Some(Duration::from_secs(10)))
        .ok()?;
    let mut reader = BufReader::new(stream.try_clone().ok()?);

    let mut request_line = String::new();
    reader.read_line(&mut request_line).ok()?;
    let mut parts = request_line.split_whitespace();
    let method = parts.next()?.to_string();
    let target = parts.next()?.to_string();

    let mut headers = BTreeMap::new();
    let mut content_length = 0usize;
    loop {
        let mut line = String::new();
        reader.read_line(&mut line).ok()?;
        let line = line.trim_end();
        if line.is_empty() {
            break;
        }
        if let Some((name, value)) = line.split_once(':') {
            let name = name.trim().to_ascii_lowercase();
            let value = value.trim().to_string();
            if name == "content-length" {
                content_length = value.parse().unwrap_or(0);
            }
            if RECORDED_HEADERS.contains(&name.as_str()) {
                headers.insert(name, value);
            }
        }
    }
    let mut body = vec![0u8; content_length];
    reader.read_exact(&mut body).ok()?;

    let path = target.split('?').next().unwrap_or("");
    let route = routes
        .iter()
        .find(|route| route.method.eq_ignore_ascii_case(&method) && route.path == path);
    let (status, response_body) = match route {
        Some(route) => (route.status, route.body.as_str()),
        None => (404, ""),
    };
    let payload = if method.eq_ignore_ascii_case("HEAD") {
        ""
    } else {
        response_body
    };
    let mut stream = stream;
    let _ = write!(
        stream,
        "HTTP/1.1 {status} {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{payload}",
        reason(status),
        payload.len()
    );
    let _ = stream.flush();

    Some(Recorded {
        method,
        target,
        headers,
        body: String::from_utf8_lossy(&body).into_owned(),
    })
}

fn reason(status: u16) -> &'static str {
    match status {
        200 => "OK",
        201 => "Created",
        204 => "No Content",
        400 => "Bad Request",
        401 => "Unauthorized",
        403 => "Forbidden",
        404 => "Not Found",
        409 => "Conflict",
        429 => "Too Many Requests",
        500 => "Internal Server Error",
        _ => "Status",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn get(base: &str, path: &str, token: &str) -> (u16, String) {
        let address = base.trim_start_matches("http://");
        let mut stream = TcpStream::connect(address).unwrap();
        write!(
            stream,
            "GET {path}?limit=5 HTTP/1.1\r\nHost: x\r\nAuthorization: {token}\r\nAccept: */*\r\n\r\n"
        )
        .unwrap();
        let mut response = String::new();
        stream.read_to_string(&mut response).unwrap();
        let status = response[9..12].parse().unwrap();
        let body = response.split("\r\n\r\n").nth(1).unwrap_or("").to_string();
        (status, body)
    }

    #[test]
    fn a_route_is_served_and_every_request_is_recorded() {
        let server = FixtureServer::start(vec![Route {
            method: "GET".into(),
            path: "/api/v2/caller-identity".into(),
            status: 200,
            body: "{\"accountId\":\"a\"}".into(),
        }])
        .unwrap();
        let base = server.base_uri.clone();
        assert_eq!(
            get(&base, "/api/v2/caller-identity", "api-token"),
            (200, "{\"accountId\":\"a\"}".to_string())
        );
        assert_eq!(get(&base, "/nope", "api-token"), (404, String::new()));
        let recorded = server.finish();
        assert_eq!(recorded.len(), 2);
        let rendered = render_requests(&recorded);
        assert_eq!(
            rendered,
            "GET /api/v2/caller-identity?limit=5\nauthorization: api-token\n---\nGET /nope?limit=5\nauthorization: api-token\n"
        );
    }
}
