//! `ldcli whoami`: the identity behind the current access token.
//!
//! A port of `cmd/whoami/whoami.go`. JSON output is the caller-identity body
//! as sent. Any other kind makes up to two more requests, for the member and
//! the account, and prints a short summary; those two are best effort.

use crate::cli::Outcome;
use crate::http::{self, Request};
use crate::output::{self, CmdOutputOpts};
use serde::Deserialize;

pub const NO_TOKEN: &str = "no access token configured. Run `ldcli login` or set LD_ACCESS_TOKEN";

pub struct WhoamiContext<'a> {
    pub access_token: &'a str,
    pub base_uri: &'a str,
    /// The effective kind: `--json` already applied.
    pub output: &'a str,
    pub version: &'a str,
}

#[derive(Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
struct CallerIdentity {
    account_id: String,
    client_id: String,
    member_id: String,
    service_token: bool,
    token_kind: String,
    token_name: String,
}

#[derive(Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
struct MemberSummary {
    email: String,
    first_name: String,
    last_name: String,
    role: String,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
struct AccountSummary {
    organization: String,
}

pub fn run(ctx: &WhoamiContext<'_>) -> Outcome {
    if ctx.access_token.is_empty() {
        return Outcome::Failure(format!("{NO_TOKEN}\n"));
    }
    let get = |path: &str| {
        http::make_request(&Request {
            access_token: ctx.access_token,
            method: "GET",
            url: &http::join_path(ctx.base_uri, path),
            content_type: "application/json",
            body: &[],
            beta: false,
            version: ctx.version,
        })
    };

    let identity_body = match get("api/v2/caller-identity") {
        Ok(body) => String::from_utf8_lossy(&body).into_owned(),
        Err(err) => {
            return Outcome::Failure(format!(
                "{}\n",
                output::cmd_output_error(ctx.output, &err.into_cmd_error())
            ))
        }
    };

    if ctx.output == "json" {
        return match output::cmd_output("get", "json", &identity_body, &CmdOutputOpts::default()) {
            Ok(rendered) => Outcome::Stdout(format!("{}\n", rendered.stdout)),
            Err(message) => Outcome::Failure(format!("{message}\n")),
        };
    }

    let identity: CallerIdentity = match serde_json::from_str(&identity_body) {
        Ok(identity) => identity,
        Err(err) => return Outcome::Failure(format!("{err}\n")),
    };
    let member = if identity.member_id.is_empty() {
        None
    } else {
        get(&format!("api/v2/members/{}", identity.member_id))
            .ok()
            .and_then(|body| serde_json::from_slice::<MemberSummary>(&body).ok())
    };
    let account = get("api/v2/account")
        .ok()
        .and_then(|body| serde_json::from_slice::<AccountSummary>(&body).ok());

    Outcome::Stdout(format!(
        "{}\n",
        format_plaintext(&identity, member.as_ref(), account.as_ref())
    ))
}

fn format_plaintext(
    identity: &CallerIdentity,
    member: Option<&MemberSummary>,
    account: Option<&AccountSummary>,
) -> String {
    let mut out = String::new();
    if let Some(member) = member {
        let name = format!("{} {}", member.first_name, member.last_name);
        let name = name.trim();
        if name.is_empty() {
            out.push_str(&format!("{}\n", member.email));
        } else {
            out.push_str(&format!("{name} <{}>\n", member.email));
        }
        out.push_str(&format!("Role:    {}\n", member.role));
    }

    let token_kind = if identity.service_token {
        "service token"
    } else {
        identity.token_kind.as_str()
    };
    if !identity.token_name.is_empty() {
        out.push_str(&format!(
            "Token:   {} ({token_kind})\n",
            identity.token_name
        ));
    } else if !identity.client_id.is_empty() {
        out.push_str(&format!("Token:   {} ({token_kind})\n", identity.client_id));
    }

    match account.filter(|account| !account.organization.is_empty()) {
        Some(account) if !identity.account_id.is_empty() => out.push_str(&format!(
            "Account: {} ({})\n",
            account.organization, identity.account_id
        )),
        Some(account) => out.push_str(&format!("Account: {}\n", account.organization)),
        None if !identity.account_id.is_empty() => {
            out.push_str(&format!("Account: {}\n", identity.account_id))
        }
        None => {}
    }
    out.trim_end_matches('\n').to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_member_with_a_name_is_shown_with_their_email() {
        let identity = CallerIdentity {
            account_id: "acct".into(),
            token_name: "ci".into(),
            token_kind: "personal".into(),
            ..CallerIdentity::default()
        };
        let member = MemberSummary {
            email: "ada@example.com".into(),
            first_name: "Ada".into(),
            last_name: "Lovelace".into(),
            role: "admin".into(),
        };
        let account = AccountSummary {
            organization: "Analytical Engines".into(),
        };
        assert_eq!(
            format_plaintext(&identity, Some(&member), Some(&account)),
            "Ada Lovelace <ada@example.com>\nRole:    admin\nToken:   ci (personal)\nAccount: Analytical Engines (acct)"
        );
    }

    #[test]
    fn a_service_token_without_a_name_falls_back_to_the_client_id() {
        let identity = CallerIdentity {
            client_id: "client-1".into(),
            service_token: true,
            token_kind: "personal".into(),
            ..CallerIdentity::default()
        };
        assert_eq!(
            format_plaintext(&identity, None, None),
            "Token:   client-1 (service token)"
        );
    }

    #[test]
    fn the_account_id_stands_in_when_the_organization_is_unknown() {
        let identity = CallerIdentity {
            account_id: "acct".into(),
            ..CallerIdentity::default()
        };
        let empty = AccountSummary::default();
        assert_eq!(
            format_plaintext(&identity, None, Some(&empty)),
            "Account: acct"
        );
    }

    #[test]
    fn no_token_is_reported_before_any_request() {
        let outcome = run(&WhoamiContext {
            access_token: "",
            base_uri: "http://127.0.0.1:1",
            output: "json",
            version: "test",
        });
        assert_eq!(outcome, Outcome::Failure(format!("{NO_TOKEN}\n")));
    }
}
