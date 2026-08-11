use std::fs;
use std::net::TcpListener;
use std::path::Path;
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::Duration;

use reqwest::{Certificate, Client, Identity};
use tokio::runtime::Runtime;

fn path(value: &Path) -> &str {
    value.to_str().expect("test path is UTF-8")
}

fn run_openssl(arguments: &[String]) {
    let status = Command::new("openssl")
        .args(arguments)
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .expect("run openssl test command");
    assert!(status.success(), "openssl command failed: {arguments:?}");
}

pub(super) fn create_ca(key: &Path, certificate: &Path, subject: &str) {
    run_openssl(&[
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

pub(super) fn create_request(key: &Path, request: &Path, subject: &str) {
    run_openssl(&[
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

pub(super) fn issue_certificate(
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
    run_openssl(&arguments);
}

pub(super) fn tls_get(
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
            .timeout(Duration::from_secs(2));
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

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .expect("bind free test port")
        .local_addr()
        .expect("read free test port")
        .port()
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
