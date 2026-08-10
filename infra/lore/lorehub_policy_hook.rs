use std::fs;
use std::future::Future;
use std::io;
use std::panic::{AssertUnwindSafe, catch_unwind};
use std::sync::mpsc::{SyncSender, sync_channel};
use std::thread::{self, JoinHandle};
use std::time::Duration;

use async_trait::async_trait;
use lore_revision::lore::BranchId;
use reqwest::{Certificate, Client, Identity, Url};
use serde::{Deserialize, Serialize};
use tokio::runtime::Runtime;

use crate::hooks::{
    Hook, HookContext, HookError, HookFactory, HookPoint, HookRegistrationContext, HookRegistry,
    StatusCode,
};

const HOOK_NAME: &str = "lorehub_policy";
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

type RuntimeJob = Box<dyn FnOnce(&Runtime) + Send + 'static>;

struct HookRuntime {
    sender: Option<SyncSender<RuntimeJob>>,
    thread: Option<JoinHandle<()>>,
}

impl HookRuntime {
    fn new() -> Result<Self, io::Error> {
        let (sender, receiver) = sync_channel::<RuntimeJob>(32);
        let (ready_sender, ready_receiver) = sync_channel(1);
        let thread = thread::Builder::new()
            .name("lorehub-policy-hook".to_string())
            .spawn(move || {
                let runtime = tokio::runtime::Builder::new_current_thread()
                    .enable_all()
                    .build();
                let ready = runtime.is_ok();
                let _ = ready_sender.send(ready);
                let Ok(runtime) = runtime else {
                    return;
                };
                while let Ok(job) = receiver.recv() {
                    let _ = catch_unwind(AssertUnwindSafe(|| job(&runtime)));
                }
            })?;
        if !ready_receiver.recv().unwrap_or(false) {
            let _ = thread.join();
            return Err(io::Error::new(
                io::ErrorKind::Other,
                "could not build policy runtime",
            ));
        }
        Ok(Self {
            sender: Some(sender),
            thread: Some(thread),
        })
    }

    fn block_on<F>(&self, future: F) -> Result<F::Output, String>
    where
        F: Future + Send + 'static,
        F::Output: Send + 'static,
    {
        let (sender, receiver) = sync_channel(1);
        let job = Box::new(move |runtime: &Runtime| {
            let _ = sender.send(runtime.block_on(future));
        });
        self.sender
            .as_ref()
            .ok_or_else(|| "policy runtime is unavailable".to_string())?
            .try_send(job)
            .map_err(|_| "policy runtime queue is full".to_string())?;
        receiver
            .recv_timeout(REQUEST_TIMEOUT)
            .map_err(|error| match error {
                std::sync::mpsc::RecvTimeoutError::Timeout => {
                    "policy runtime timed out".to_string()
                }
                std::sync::mpsc::RecvTimeoutError::Disconnected => {
                    "policy runtime stopped unexpectedly".to_string()
                }
            })
    }
}

impl Drop for HookRuntime {
    fn drop(&mut self) {
        self.sender.take();
        if let Some(thread) = self.thread.take() {
            let _ = thread.join();
        }
    }
}

struct LoreHubPolicyHook {
    client: Client,
    runtime: HookRuntime,
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
            branch_name: context.get_metadata("branch_name").map(ToString::to_string),
            proposed_revision: context.revision().map(|revision| revision.to_string()),
            client_ip: context.get_metadata("client_ip").map(ToString::to_string),
        };
        let body = serde_json::to_vec(&request)
            .map_err(|_| "could not serialize policy request".to_string())?;
        let client = self.client.clone();
        let endpoint = self.policy_endpoint.clone();
        self.runtime
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
            .and_then(|result| result)
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
        let environment = required_config_string(self.name(), config, "environment")?;
        let root_domain = required_config_string(self.name(), config, "root_domain")?;
        let policy_endpoint = required_config_string(self.name(), config, "endpoint")?;
        let observation_endpoint =
            required_config_string(self.name(), config, "observation_endpoint")?;
        let auth_endpoint = required_config_string(self.name(), config, "auth_endpoint")?;
        let jwks_endpoint = required_config_string(self.name(), config, "jwks_endpoint")?;

        validate_root_domain(&root_domain, &environment)
            .map_err(|message| HookError::config_error(self.name(), message))?;
        validate_http_endpoint(
            "endpoint",
            &policy_endpoint,
            "/internal/lore/policy",
            &root_domain,
            &environment,
        )
        .map_err(|message| HookError::config_error(self.name(), message))?;
        validate_http_endpoint(
            "observation_endpoint",
            &observation_endpoint,
            "/internal/lore/observation",
            &root_domain,
            &environment,
        )
        .map_err(|message| HookError::config_error(self.name(), message))?;
        validate_auth_endpoint(&auth_endpoint, &root_domain, &environment)
            .map_err(|message| HookError::config_error(self.name(), message))?;
        validate_http_endpoint(
            "jwks_endpoint",
            &jwks_endpoint,
            "/.well-known/jwks.json",
            &root_domain,
            &environment,
        )
        .map_err(|message| HookError::config_error(self.name(), message))?;

        let ca_path = required_config_string(self.name(), config, "ca_certificate")?;
        let client_cert_path = required_config_string(self.name(), config, "client_certificate")?;
        let client_key_path = required_config_string(self.name(), config, "client_key")?;
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
        let runtime = HookRuntime::new()
            .map_err(|_| HookError::init_error(self.name(), "could not build policy runtime"))?;
        Ok(Box::new(LoreHubPolicyHook {
            client,
            runtime,
            policy_endpoint,
            observation_endpoint,
        }))
    }
}

fn required_config_string(
    hook_name: &str,
    config: &toml::Value,
    key: &str,
) -> Result<String, HookError> {
    config
        .get(key)
        .and_then(toml::Value::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToString::to_string)
        .ok_or_else(|| HookError::config_error(hook_name, format!("{key} is required")))
}

fn validate_root_domain(root_domain: &str, environment: &str) -> Result<(), String> {
    let root = normalize_host(root_domain)?;
    if root.contains(':') || root.contains('/') || root.contains('@') || root.contains(' ') {
        return Err("root_domain must be a DNS host".to_string());
    }
    if environment == "production" && (root == "localhost" || root.ends_with(".localhost")) {
        return Err("production root_domain cannot be localhost".to_string());
    }
    match environment {
        "production" | "development" | "local-insecure" => Ok(()),
        _ => Err("environment must be production, development, or local-insecure".to_string()),
    }
}

fn validate_http_endpoint(
    name: &str,
    value: &str,
    expected_path: &str,
    root_domain: &str,
    environment: &str,
) -> Result<(), String> {
    let url = Url::parse(value).map_err(|_| format!("{name} must be a valid URL"))?;
    let scheme_allowed =
        url.scheme() == "https" || (environment != "production" && url.scheme() == "http");
    if !scheme_allowed {
        return Err(format!("{name} must use HTTPS in production"));
    }
    if url.username() != ""
        || url.password().is_some()
        || url.query().is_some()
        || url.fragment().is_some()
    {
        return Err(format!(
            "{name} must not contain credentials, query, or fragment"
        ));
    }
    let host = url
        .host_str()
        .ok_or_else(|| format!("{name} must contain a host"))?;
    if !host_within_root(host, root_domain) || url.path() != expected_path {
        return Err(format!(
            "{name} must use the configured root and fixed path"
        ));
    }
    Ok(())
}

fn validate_auth_endpoint(value: &str, root_domain: &str, environment: &str) -> Result<(), String> {
    let url = Url::parse(value).map_err(|_| "auth_endpoint must be a valid URL".to_string())?;
    let scheme_allowed = matches!(url.scheme(), "ucs-auth" | "https")
        || (environment != "production" && url.scheme() == "http");
    if !scheme_allowed
        || url.username() != ""
        || url.password().is_some()
        || (!url.path().is_empty() && url.path() != "/")
        || url.query().is_some()
        || url.fragment().is_some()
    {
        return Err("auth_endpoint must be a fixed managed endpoint".to_string());
    }
    let host = url
        .host_str()
        .ok_or_else(|| "auth_endpoint must contain a host".to_string())?;
    if !host_within_root(host, root_domain) {
        return Err("auth_endpoint must use the configured root domain".to_string());
    }
    Ok(())
}

fn normalize_host(value: &str) -> Result<String, String> {
    let value = value.trim().trim_end_matches('.').to_ascii_lowercase();
    let labels = value.split('.').collect::<Vec<_>>();
    if value.is_empty()
        || (value != "localhost" && labels.len() < 2)
        || labels.iter().any(|label| label.is_empty())
    {
        return Err("root_domain must be a non-empty DNS host".to_string());
    }
    if value.len() > 253
        || labels.iter().any(|label| {
            label.len() > 63
                || label.starts_with('-')
                || label.ends_with('-')
                || !label
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-')
        })
    {
        return Err("root_domain must be a DNS host".to_string());
    }
    Ok(value)
}

fn host_within_root(host: &str, root_domain: &str) -> bool {
    let Ok(host) = normalize_host(host) else {
        return false;
    };
    let Ok(root) = normalize_host(root_domain) else {
        return false;
    };
    host == root || host.ends_with(&format!(".{root}"))
}

pub fn register(registry: &mut HookRegistry, _context: &HookRegistrationContext) {
    registry.register_hook(Box::new(LoreHubPolicyFactory));
}

#[cfg(test)]
mod tests {
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::path::Path;
    use std::process::{Child, Command, Stdio};
    use std::sync::mpsc;
    use std::thread;
    use std::time::{SystemTime, UNIX_EPOCH};

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
        let status = status.to_owned();
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
            runtime: HookRuntime::new().expect("build test policy runtime"),
            policy_endpoint,
            observation_endpoint,
        }
    }

    fn factory_config(environment: &str, policy: &str, observation: &str) -> toml::Value {
        let value = format!(
            concat!(
                "environment = \"{}\"\n",
                "root_domain = \"control.test\"\n",
                "endpoint = \"{}\"\n",
                "observation_endpoint = \"{}\"\n",
                "auth_endpoint = \"https://api.control.test:8443\"\n",
                "jwks_endpoint = \"https://api.control.test/.well-known/jwks.json\"\n",
                "ca_certificate = \"/missing/ca.crt\"\n",
                "client_certificate = \"/missing/client.crt\"\n",
                "client_key = \"/missing/client.key\"\n"
            ),
            environment, policy, observation
        );
        toml::from_str(&value).expect("parse hook configuration")
    }

    #[test]
    fn protected_push_is_rejected_by_the_policy_response() {
        let (endpoint, _) = response_server("403 Forbidden", "{}", Duration::ZERO);
        let policy_hook = hook(endpoint, "http://127.0.0.1:1/observation".to_string());
        assert!(
            policy_hook
                .pre_handler(&context(HookPoint::BranchPush))
                .is_err()
        );
    }

    #[test]
    fn feature_push_is_allowed_by_the_policy_response() {
        let (endpoint, _) = response_server("200 OK", r#"{"allowed":true}"#, Duration::ZERO);
        let policy_hook = hook(endpoint, "http://127.0.0.1:1/observation".to_string());
        policy_hook
            .pre_handler(&context(HookPoint::BranchPush))
            .expect("an authorized feature push succeeds");
    }

    #[tokio::test(flavor = "current_thread")]
    async fn pre_handler_is_safe_when_called_inside_a_tokio_runtime() {
        let (allow_endpoint, _) = response_server("200 OK", r#"{"allowed":true}"#, Duration::ZERO);
        let allow_hook = hook(allow_endpoint, "http://127.0.0.1:1/observation".to_string());
        allow_hook
            .pre_handler(&context(HookPoint::BranchPush))
            .expect("an authorized request succeeds from an async Lore handler");

        let (deny_endpoint, _) = response_server("403 Forbidden", "{}", Duration::ZERO);
        let deny_hook = hook(deny_endpoint, "http://127.0.0.1:1/observation".to_string());
        assert!(
            deny_hook
                .pre_handler(&context(HookPoint::BranchPush))
                .is_err()
        );
    }

    #[test]
    fn unavailable_policy_endpoint_is_rejected_after_the_fixed_timeout() {
        let (endpoint, _) =
            response_server("200 OK", r#"{"allowed":true}"#, Duration::from_millis(500));
        let policy_hook = hook(endpoint, "http://127.0.0.1:1/observation".to_string());
        assert!(
            policy_hook
                .pre_handler(&context(HookPoint::BranchPush))
                .is_err()
        );
    }

    #[test]
    fn successful_push_post_handler_sends_the_observation() {
        let (endpoint, received) = response_server("204 No Content", "", Duration::ZERO);
        let policy_hook = hook("http://127.0.0.1:1/policy".to_string(), endpoint);
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .expect("build post-handler test runtime");
        runtime
            .block_on(policy_hook.post_handler(&context(HookPoint::BranchPush)))
            .expect("post observation succeeds");
        let request = received.recv().expect("receive observation request");
        let request = String::from_utf8_lossy(&request);
        assert!(request.contains("branchId"));
        assert!(request.contains("revision"));
        assert!(request.contains("branch_push"));
    }

    #[test]
    fn custom_managed_domain_is_accepted() {
        assert!(
            validate_http_endpoint(
                "endpoint",
                "https://policy.control.test/internal/lore/policy",
                "/internal/lore/policy",
                "control.test",
                "production",
            )
            .is_ok()
        );
    }

    #[test]
    fn sibling_and_evil_suffix_domains_are_rejected() {
        for host in ["policy.control.test.evil", "policy-control.test"] {
            let endpoint = format!("https://{host}/internal/lore/policy");
            assert!(
                validate_http_endpoint(
                    "endpoint",
                    &endpoint,
                    "/internal/lore/policy",
                    "control.test",
                    "production",
                )
                .is_err()
            );
        }
    }

    #[test]
    fn production_http_endpoint_is_rejected() {
        assert!(
            validate_http_endpoint(
                "endpoint",
                "http://policy.control.test/internal/lore/policy",
                "/internal/lore/policy",
                "control.test",
                "production",
            )
            .is_err()
        );
    }

    #[test]
    fn internal_https_auth_endpoint_is_accepted_and_production_http_is_rejected() {
        assert!(
            validate_auth_endpoint(
                "https://api.control.test:8443",
                "control.test",
                "production",
            )
            .is_ok()
        );
        assert!(
            validate_auth_endpoint("http://api.control.test:8443", "control.test", "production",)
                .is_err()
        );
    }

    #[test]
    fn missing_mtls_material_is_rejected() {
        let factory = LoreHubPolicyFactory;
        let config = factory_config(
            "production",
            "https://policy.control.test/internal/lore/policy",
            "https://observe.control.test/internal/lore/observation",
        );
        let result = factory.create(&config);
        assert!(result.is_err(), "missing mTLS must fail");
        let error = result.err().expect("missing mTLS error");
        assert!(error.to_string().contains("policy CA"));
    }

    #[test]
    fn tls_policy_client_requires_server_san_and_a_valid_client_certificate() {
        let directory = std::env::temp_dir().join(format!(
            "lorehub-hook-san-{}",
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .expect("read test clock")
                .as_nanos()
        ));
        fs::create_dir(&directory).expect("create SAN test directory");
        let ca_key = directory.join("ca.key");
        let ca_certificate = directory.join("ca.crt");
        let server_key = directory.join("server.key");
        let server_request = directory.join("server.csr");
        let server_certificate = directory.join("server-localhost.crt");
        let server_wrong_certificate = directory.join("server-wrong.crt");
        let server_extensions = directory.join("server-localhost.ext");
        let server_wrong_extensions = directory.join("server-wrong.ext");
        let client_key = directory.join("client.key");
        let client_request = directory.join("client.csr");
        let client_certificate = directory.join("client.crt");
        let client_extensions = directory.join("client.ext");
        let ca_serial = directory.join("ca.srl");
        fs::write(
            &server_extensions,
            "subjectAltName=DNS:localhost\nextendedKeyUsage=serverAuth\n",
        )
        .expect("write server certificate extensions");
        fs::write(
            &server_wrong_extensions,
            "subjectAltName=DNS:wrong.localhost\nextendedKeyUsage=serverAuth\n",
        )
        .expect("write wrong server certificate extensions");
        fs::write(
            &client_extensions,
            "basicConstraints=critical,CA:FALSE\nextendedKeyUsage=clientAuth\n",
        )
        .expect("write client certificate extensions");
        create_ca(&ca_key, &ca_certificate, "/CN=LoreHub test CA");
        create_request(&server_key, &server_request, "/CN=localhost");
        issue_certificate(
            &server_request,
            &ca_certificate,
            &ca_key,
            &server_certificate,
            &server_extensions,
            &ca_serial,
            true,
        );
        issue_certificate(
            &server_request,
            &ca_certificate,
            &ca_key,
            &server_wrong_certificate,
            &server_wrong_extensions,
            &ca_serial,
            false,
        );
        create_request(&client_key, &client_request, "/CN=lorehub-hook-test-client");
        issue_certificate(
            &client_request,
            &ca_certificate,
            &ca_key,
            &client_certificate,
            &client_extensions,
            &ca_serial,
            false,
        );
        assert!(
            tls_get(
                &directory,
                &server_certificate,
                &server_key,
                &ca_certificate,
                Some((&client_certificate, &client_key)),
            )
            .expect("valid mTLS request")
            .is_success()
        );
        assert!(
            tls_get(
                &directory,
                &server_wrong_certificate,
                &server_key,
                &ca_certificate,
                Some((&client_certificate, &client_key)),
            )
            .is_err()
        );
        assert!(
            tls_get(
                &directory,
                &server_certificate,
                &server_key,
                &ca_certificate,
                None,
            )
            .is_err()
        );
        assert!(
            tls_get(
                &directory,
                &server_certificate,
                &server_key,
                &ca_certificate,
                Some((&server_certificate, &server_key)),
            )
            .is_err()
        );
        let _ = fs::remove_dir_all(directory);
    }
    fn path(value: &Path) -> &str {
        value.to_str().expect("test path is UTF-8")
    }
    fn run_openssl_owned(arguments: &[String]) {
        let status = Command::new("openssl")
            .args(arguments)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .expect("run openssl test command");
        assert!(status.success(), "openssl command failed: {arguments:?}");
    }
    fn create_ca(key: &Path, certificate: &Path, subject: &str) {
        run_openssl_owned(&[
            "req".to_string(),
            "-x509".to_string(),
            "-newkey".to_string(),
            "rsa:2048".to_string(),
            "-nodes".to_string(),
            "-keyout".to_string(),
            path(key).to_string(),
            "-out".to_string(),
            path(certificate).to_string(),
            "-subj".to_string(),
            subject.to_string(),
            "-days".to_string(),
            "1".to_string(),
            "-addext".to_string(),
            "basicConstraints=critical,CA:TRUE".to_string(),
            "-addext".to_string(),
            "keyUsage=critical,keyCertSign,cRLSign".to_string(),
        ]);
    }

    fn create_request(key: &Path, request: &Path, subject: &str) {
        run_openssl_owned(&[
            "req".to_string(),
            "-newkey".to_string(),
            "rsa:2048".to_string(),
            "-nodes".to_string(),
            "-keyout".to_string(),
            path(key).to_string(),
            "-out".to_string(),
            path(request).to_string(),
            "-subj".to_string(),
            subject.to_string(),
        ]);
    }

    fn issue_certificate(
        request: &Path,
        ca: &Path,
        ca_key: &Path,
        certificate: &Path,
        extensions: &Path,
        serial: &Path,
        create_serial: bool,
    ) {
        let mut arguments = vec![
            "x509".to_string(),
            "-req".to_string(),
            "-in".to_string(),
            path(request).to_string(),
            "-CA".to_string(),
            path(ca).to_string(),
            "-CAkey".to_string(),
            path(ca_key).to_string(),
        ];
        if create_serial {
            arguments.push("-CAcreateserial".to_string());
        } else {
            arguments.extend(["-CAserial".to_string(), path(serial).to_string()]);
        }
        arguments.extend([
            "-out".to_string(),
            path(certificate).to_string(),
            "-days".to_string(),
            "1".to_string(),
            "-extfile".to_string(),
            path(extensions).to_string(),
        ]);
        run_openssl_owned(&arguments);
    }

    fn free_port() -> u16 {
        TcpListener::bind("127.0.0.1:0")
            .expect("bind free test port")
            .local_addr()
            .expect("read free test port")
            .port()
    }

    fn tls_get(
        directory: &Path,
        server_certificate: &Path,
        server_key: &Path,
        ca_certificate: &Path,
        client_identity: Option<(&Path, &Path)>,
    ) -> Result<reqwest::StatusCode, String> {
        let (mut server, port) =
            start_tls_server(directory, server_certificate, server_key, ca_certificate);
        let result = (|| {
            let ca = fs::read(ca_certificate).map_err(|error| error.to_string())?;
            let ca = Certificate::from_pem(&ca).map_err(|error| error.to_string())?;
            let mut builder = Client::builder()
                .add_root_certificate(ca)
                .timeout(REQUEST_TIMEOUT);
            if let Some((certificate_path, key_path)) = client_identity {
                let mut identity = fs::read(certificate_path).map_err(|error| error.to_string())?;
                identity.extend_from_slice(&fs::read(key_path).map_err(|error| error.to_string())?);
                let identity = Identity::from_pem(&identity).map_err(|error| error.to_string())?;
                builder = builder.identity(identity);
            }
            let client = builder.build().map_err(|error| error.to_string())?;
            let runtime = Runtime::new().map_err(|error| error.to_string())?;
            runtime.block_on(async move {
                client
                    .get(format!("https://localhost:{port}/"))
                    .send()
                    .await
                    .map(|response| response.status())
                    .map_err(|error| error.to_string())
            })
        })();
        stop_server(&mut server);
        result
    }

    fn start_tls_server(
        directory: &Path,
        server_certificate: &Path,
        server_key: &Path,
        ca_certificate: &Path,
    ) -> (Child, u16) {
        let port = free_port();
        let port_argument = port.to_string();
        let server = Command::new("openssl")
            .current_dir(directory)
            .args([
                "s_server",
                "-accept",
                &port_argument,
                "-cert",
                path(server_certificate),
                "-key",
                path(server_key),
                "-Verify",
                "1",
                "-verifyCAfile",
                path(ca_certificate),
                "-verify_return_error",
                "-www",
                "-quiet",
                "-naccept",
                "1",
            ])
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .expect("start mTLS test server");
        thread::sleep(Duration::from_millis(150));
        (server, port)
    }
    fn stop_server(server: &mut Child) {
        let _ = server.kill();
        let _ = server.wait();
    }
    #[test]
    fn hook_runtime_drops_safely_inside_lore_async_shutdown() {
        let lore_runtime = Runtime::new().expect("build Lore test runtime");
        lore_runtime.block_on(async {
            drop(HookRuntime::new().expect("build hook runtime"));
        });
    }

    #[test]
    fn hook_runtime_rejects_when_worker_thread_is_stopped() {
        let mut hook_runtime = HookRuntime::new().expect("build hook runtime");
        hook_runtime.sender.take();
        hook_runtime
            .thread
            .take()
            .expect("hook runtime thread")
            .join()
            .expect("hook runtime worker exits cleanly");

        let result = hook_runtime.block_on(async { () });
        assert!(result.is_err(), "a stopped worker must fail closed");
    }
}
