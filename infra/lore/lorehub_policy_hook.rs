use std::fs;
use std::time::Duration;

use async_trait::async_trait;
use lore_revision::lore::BranchId;
use reqwest::{Certificate, Client, Identity};
use serde::{Deserialize, Serialize};
use tokio::runtime::Runtime;

use crate::hooks::{
    Hook, HookContext, HookError, HookFactory, HookPoint, HookRegistrationContext, HookRegistry,
    StatusCode,
};

const HOOK_NAME: &str = "lorehub_policy";
const DEFAULT_POLICY_ENDPOINT: &str = "https://api.lorehub.localhost:8444/internal/lore/policy";
const DEFAULT_OBSERVATION_ENDPOINT: &str =
    "https://api.lorehub.localhost:8444/internal/lore/observation";
const PRODUCTION_POLICY_ENDPOINT: &str = "https://api.lorehub.example:8444/internal/lore/policy";
const PRODUCTION_OBSERVATION_ENDPOINT: &str =
    "https://api.lorehub.example:8444/internal/lore/observation";
const REQUEST_TIMEOUT: Duration = Duration::from_millis(150);
const HOOK_POINTS: &[HookPoint] = &[
    HookPoint::BranchPush,
    HookPoint::BranchCreate,
    HookPoint::BranchDelete,
    HookPoint::RepositoryCreate,
    HookPoint::Obliterate,
];

#[derive(Serialize)]
struct PolicyRequest {
    #[serde(rename = "userId")]
    user_id: String,
    #[serde(rename = "resourceId")]
    resource_id: String,
    operation: String,
    #[serde(rename = "branchId")]
    branch_id: Option<String>,
    #[serde(rename = "proposedRevision")]
    proposed_revision: Option<String>,
    #[serde(rename = "clientIp")]
    client_ip: Option<String>,
}

#[derive(Serialize)]
struct ObservationRequest {
    #[serde(rename = "userId")]
    user_id: String,
    #[serde(rename = "resourceId")]
    resource_id: String,
    operation: String,
    #[serde(rename = "branchId")]
    branch_id: String,
    revision: Option<String>,
}

#[derive(Deserialize)]
struct PolicyResponse {
    allowed: bool,
}

struct LoreHubPolicyHook {
    client: Client,
    runtime: Runtime,
    policy_endpoint: String,
    observation_endpoint: String,
}

impl LoreHubPolicyHook {
    fn operation(context: &HookContext) -> &'static str {
        match context.hook_point() {
            HookPoint::BranchPush => "branch_push",
            HookPoint::BranchCreate => "branch_create",
            HookPoint::BranchDelete => "branch_delete",
            HookPoint::RepositoryCreate => "repository_create",
            HookPoint::Obliterate => "obliterate",
        }
    }

    fn request(&self, context: &HookContext) -> Result<(), String> {
        let user_id = context
            .user()
            .ok_or_else(|| "missing authenticated user".to_string())?;
        let request = PolicyRequest {
            user_id: user_id.to_string(),
            resource_id: format!("urc-{}", context.repository()),
            operation: Self::operation(context).to_string(),
            branch_id: context.branch().map(branch_id),
            proposed_revision: context.revision().map(|revision| revision.to_string()),
            client_ip: context.get_metadata("client_ip").map(ToString::to_string),
        };
        let body = serde_json::to_vec(&request)
            .map_err(|_| "could not serialize policy request".to_string())?;
        self.runtime.block_on(async {
            let response = self
                .client
                .post(&self.policy_endpoint)
                .header(reqwest::header::CONTENT_TYPE, "application/json")
                .body(body)
                .send()
                .await
                .map_err(|_| "policy endpoint unavailable".to_string())?;
            if !response.status().is_success() {
                return Err("policy denied the Lore operation".to_string());
            }
            let body = response
                .bytes()
                .await
                .map_err(|_| "policy response was invalid".to_string())?;
            let decision: PolicyResponse = serde_json::from_slice(&body)
                .map_err(|_| "policy response was invalid".to_string())?;
            if !decision.allowed {
                return Err("policy denied the Lore operation".to_string());
            }
            Ok(())
        })
    }

    async fn post_request(&self, context: &HookContext) -> Result<(), HookError> {
        let operation = match context.hook_point() {
            HookPoint::BranchPush => "branch_push",
            HookPoint::BranchDelete => "branch_delete",
            _ => return Ok(()),
        };
        let branch_id = context.branch().map(branch_id).ok_or_else(|| {
            HookError::init_error(self.name(), "branch observation has no branch ID")
        })?;
        let user_id = context
            .user()
            .ok_or_else(|| HookError::init_error(self.name(), "branch observation has no user"))?;
        let request = ObservationRequest {
            user_id: user_id.to_string(),
            resource_id: format!("urc-{}", context.repository()),
            operation: operation.to_string(),
            branch_id,
            revision: context.revision().map(|revision| revision.to_string()),
        };
        let body = serde_json::to_vec(&request)
            .map_err(|_| HookError::init_error(self.name(), "could not serialize observation"))?;
        let response = self
            .client
            .post(&self.observation_endpoint)
            .header(reqwest::header::CONTENT_TYPE, "application/json")
            .body(body)
            .send()
            .await
            .map_err(|_| {
                HookError::init_error(self.name(), "branch observation endpoint unavailable")
            })?;
        if !response.status().is_success() {
            return Err(HookError::init_error(
                self.name(),
                "branch observation was rejected",
            ));
        }
        Ok(())
    }
}

fn branch_id(branch: BranchId) -> String {
    branch.to_string()
}

#[async_trait]
impl Hook for LoreHubPolicyHook {
    fn name(&self) -> &'static str {
        HOOK_NAME
    }

    fn hook_points(&self) -> &'static [HookPoint] {
        HOOK_POINTS
    }

    fn pre_handler(&self, context: &HookContext) -> Result<(), HookError> {
        self.request(context).map_err(|_| {
            HookError::rejected(
                self.name(),
                "LoreHub policy could not authorize this operation",
                StatusCode::PermissionDenied,
            )
        })
    }

    async fn post_handler(&self, context: &HookContext) -> Result<(), HookError> {
        self.post_request(context).await
    }
}

struct LoreHubPolicyFactory;

impl HookFactory for LoreHubPolicyFactory {
    fn name(&self) -> &'static str {
        HOOK_NAME
    }

    fn create(&self, config: &toml::Value) -> Result<Box<dyn Hook>, HookError> {
        let policy_endpoint = config
            .get("endpoint")
            .and_then(toml::Value::as_str)
            .unwrap_or(DEFAULT_POLICY_ENDPOINT)
            .to_string();
        let observation_endpoint = config
            .get("observation_endpoint")
            .and_then(toml::Value::as_str)
            .unwrap_or(DEFAULT_OBSERVATION_ENDPOINT)
            .to_string();
        let local_endpoints = policy_endpoint == DEFAULT_POLICY_ENDPOINT
            && observation_endpoint == DEFAULT_OBSERVATION_ENDPOINT;
        let production_endpoints = policy_endpoint == PRODUCTION_POLICY_ENDPOINT
            && observation_endpoint == PRODUCTION_OBSERVATION_ENDPOINT;
        if !local_endpoints && !production_endpoints {
            return Err(HookError::config_error(
                self.name(),
                "policy endpoints must be the fixed internal LoreHub endpoints",
            ));
        }
        let ca_path = config
            .get("ca_certificate")
            .and_then(toml::Value::as_str)
            .ok_or_else(|| HookError::config_error(self.name(), "ca_certificate is required"))?;
        let client_cert_path = config
            .get("client_certificate")
            .and_then(toml::Value::as_str)
            .ok_or_else(|| {
                HookError::config_error(self.name(), "client_certificate is required")
            })?;
        let client_key_path = config
            .get("client_key")
            .and_then(toml::Value::as_str)
            .ok_or_else(|| HookError::config_error(self.name(), "client_key is required"))?;
        let ca = fs::read(ca_path)
            .map_err(|_| HookError::init_error(self.name(), "could not read policy CA"))?;
        let certificate = fs::read(client_cert_path).map_err(|_| {
            HookError::init_error(self.name(), "could not read policy client certificate")
        })?;
        let key = fs::read(client_key_path)
            .map_err(|_| HookError::init_error(self.name(), "could not read policy client key"))?;
        let mut identity = certificate;
        identity.extend_from_slice(&key);
        let identity = Identity::from_pem(&identity).map_err(|_| {
            HookError::init_error(self.name(), "could not parse policy client identity")
        })?;
        let ca = Certificate::from_pem(&ca)
            .map_err(|_| HookError::init_error(self.name(), "could not parse policy CA"))?;
        let client = Client::builder()
            .add_root_certificate(ca)
            .identity(identity)
            .timeout(REQUEST_TIMEOUT)
            .build()
            .map_err(|_| HookError::init_error(self.name(), "could not build policy client"))?;
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(|_| HookError::init_error(self.name(), "could not build policy runtime"))?;
        Ok(Box::new(LoreHubPolicyHook {
            client,
            runtime,
            policy_endpoint,
            observation_endpoint,
        }))
    }
}

pub fn register(registry: &mut HookRegistry, _context: &HookRegistrationContext) {
    registry.register_hook(Box::new(LoreHubPolicyFactory));
}

#[cfg(test)]
mod tests {
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::sync::mpsc;
    use std::thread;

    use lore_base::types::Hash;
    use lore_revision::lore::{BranchId, RepositoryId};

    use super::*;

    fn context(point: HookPoint) -> HookContext {
        HookContext::builder()
            .correlation_id("hook-test")
            .hook_point(point)
            .repository(RepositoryId::default())
            .user("user-test")
            .branch(BranchId::default())
            .revision(Hash::default())
            .metadata("client_ip", "127.0.0.1")
            .build()
    }

    fn response_server(
        status: &str,
        body: &str,
        delay: Duration,
    ) -> (String, mpsc::Receiver<Vec<u8>>) {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind test policy server");
        let address = format!(
            "http://{}",
            listener.local_addr().expect("read test address")
        );
        let (sender, receiver) = mpsc::channel();
        let body = body.as_bytes().to_vec();
        thread::spawn(move || {
            let (mut stream, _) = listener.accept().expect("accept test policy request");
            let mut request = [0_u8; 8192];
            let size = stream.read(&mut request).unwrap_or(0);
            let _ = sender.send(request[..size].to_vec());
            thread::sleep(delay);
            let response = format!(
                "HTTP/1.1 {status}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                body.len()
            );
            let _ = stream.write_all(response.as_bytes());
            let _ = stream.write_all(&body);
        });
        (address, receiver)
    }

    fn hook(policy_endpoint: String, observation_endpoint: String) -> LoreHubPolicyHook {
        LoreHubPolicyHook {
            client: Client::builder()
                .timeout(REQUEST_TIMEOUT)
                .build()
                .expect("build test policy client"),
            runtime: tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .expect("build test policy runtime"),
            policy_endpoint,
            observation_endpoint,
        }
    }

    #[test]
    fn protected_push_is_rejected_by_the_policy_response() {
        let (endpoint, _) = response_server("403 Forbidden", "{}", Duration::ZERO);
        let policy_hook = hook(endpoint, "http://127.0.0.1:1/observation".to_string());
        assert!(policy_hook
            .pre_handler(&context(HookPoint::BranchPush))
            .is_err());
    }

    #[test]
    fn feature_push_is_allowed_by_the_policy_response() {
        let (endpoint, _) = response_server("200 OK", r#"{"allowed":true}"#, Duration::ZERO);
        let policy_hook = hook(endpoint, "http://127.0.0.1:1/observation".to_string());
        policy_hook
            .pre_handler(&context(HookPoint::BranchPush))
            .expect("an authorized feature push succeeds");
    }

    #[test]
    fn unavailable_policy_endpoint_is_rejected_after_the_fixed_timeout() {
        let (endpoint, _) =
            response_server("200 OK", r#"{"allowed":true}"#, Duration::from_millis(500));
        let policy_hook = hook(endpoint, "http://127.0.0.1:1/observation".to_string());
        assert!(policy_hook
            .pre_handler(&context(HookPoint::BranchPush))
            .is_err());
    }

    #[test]
    fn successful_push_post_handler_sends_the_observation() {
        let (endpoint, received) = response_server("204 No Content", "", Duration::ZERO);
        let policy_hook = hook("http://127.0.0.1:1/policy".to_string(), endpoint);
        policy_hook
            .runtime
            .block_on(policy_hook.post_handler(&context(HookPoint::BranchPush)))
            .expect("post observation succeeds");
        let request = received.recv().expect("receive observation request");
        let request = String::from_utf8_lossy(&request);
        assert!(request.contains("branchId"));
        assert!(request.contains("revision"));
        assert!(request.contains("branch_push"));
    }
}
