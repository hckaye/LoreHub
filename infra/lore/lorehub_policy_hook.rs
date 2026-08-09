use std::fs;
use std::time::Duration;

use async_trait::async_trait;
use lore_revision::lore::BranchId;
use reqwest::{Certificate, Client, Identity};
use serde::Serialize;

use crate::hooks::{
    Hook, HookContext, HookError, HookFactory, HookPoint, HookRegistrationContext, HookRegistry,
    StatusCode,
};

const HOOK_NAME: &str = "lorehub_policy";
const DEFAULT_ENDPOINT: &str = "https://api:8444/internal/lore/policy";
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
    #[serde(rename = "branchName")]
    branch_name: Option<String>,
    #[serde(rename = "currentRevision")]
    current_revision: Option<String>,
    #[serde(rename = "proposedRevision")]
    proposed_revision: Option<String>,
    #[serde(rename = "operationAuthorization")]
    operation_authorization: Option<String>,
}

#[derive(serde::Deserialize)]
struct PolicyResponse {
    allowed: bool,
}

struct LoreHubPolicyHook {
    client: Client,
    endpoint: String,
}

impl LoreHubPolicyHook {
    fn request(&self, context: &HookContext) -> Result<(), String> {
        let operation = match context.hook_point() {
            HookPoint::BranchPush => "branch_push",
            HookPoint::BranchCreate => "branch_create",
            HookPoint::BranchDelete => "branch_delete",
            HookPoint::RepositoryCreate => "repository_create",
            HookPoint::Obliterate => "obliterate",
        };
        let user_id = context.user().ok_or_else(|| "missing authenticated user".to_string())?;
        let request = PolicyRequest {
            user_id: user_id.to_string(),
            resource_id: format!("urc-{}", context.repository()),
            operation: operation.to_string(),
            branch_id: context.branch().map(branch_id),
            branch_name: context.get_metadata("branch_name").map(ToString::to_string),
            current_revision: context.get_metadata("current_revision").map(ToString::to_string),
            proposed_revision: context.revision().map(|revision| revision.to_string()),
            operation_authorization: context
                .get_metadata("operation_authorization")
                .map(ToString::to_string),
        };
        let client = self.client.clone();
        let endpoint = self.endpoint.clone();
        let result = std::thread::spawn(move || {
            let body = serde_json::to_vec(&request)
                .map_err(|_| "could not serialize policy request".to_string())?;
            let runtime = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .map_err(|_| "could not create policy client".to_string())?;
            runtime
                .block_on(async move {
                    let response = client
                        .post(endpoint)
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
        })
        .join()
        .map_err(|_| "policy client failed".to_string())?;
        result
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
}

struct LoreHubPolicyFactory;

impl HookFactory for LoreHubPolicyFactory {
    fn name(&self) -> &'static str {
        HOOK_NAME
    }

    fn create(&self, config: &toml::Value) -> Result<Box<dyn Hook>, HookError> {
        let endpoint = config
            .get("endpoint")
            .and_then(toml::Value::as_str)
            .unwrap_or(DEFAULT_ENDPOINT)
            .to_string();
        if endpoint != DEFAULT_ENDPOINT {
            return Err(HookError::config_error(
                self.name(),
                "policy endpoint must be the fixed internal LoreHub endpoint",
            ));
        }
        let ca_path = config
            .get("ca_certificate")
            .and_then(toml::Value::as_str)
            .ok_or_else(|| HookError::config_error(self.name(), "ca_certificate is required"))?;
        let client_cert_path = config
            .get("client_certificate")
            .and_then(toml::Value::as_str)
            .ok_or_else(|| HookError::config_error(self.name(), "client_certificate is required"))?;
        let client_key_path = config
            .get("client_key")
            .and_then(toml::Value::as_str)
            .ok_or_else(|| HookError::config_error(self.name(), "client_key is required"))?;
        let ca = fs::read(ca_path)
            .map_err(|_| HookError::init_error(self.name(), "could not read policy CA"))?;
        let certificate = fs::read(client_cert_path)
            .map_err(|_| HookError::init_error(self.name(), "could not read policy client certificate"))?;
        let key = fs::read(client_key_path)
            .map_err(|_| HookError::init_error(self.name(), "could not read policy client key"))?;
        let mut identity = certificate;
        identity.extend_from_slice(&key);
        let identity = Identity::from_pem(&identity)
            .map_err(|_| HookError::init_error(self.name(), "could not parse policy client identity"))?;
        let ca = Certificate::from_pem(&ca)
            .map_err(|_| HookError::init_error(self.name(), "could not parse policy CA"))?;
        let client = Client::builder()
            .add_root_certificate(ca)
            .identity(identity)
            .timeout(REQUEST_TIMEOUT)
            .build()
            .map_err(|_| HookError::init_error(self.name(), "could not build policy client"))?;
        Ok(Box::new(LoreHubPolicyHook { client, endpoint }))
    }
}

pub fn register(registry: &mut HookRegistry, _context: &HookRegistrationContext) {
    registry.register_hook(Box::new(LoreHubPolicyFactory));
}
