// Structural validation for the Keycloak infrastructure. Runs without Docker or
// external provider secrets, and does not rely on file hashes. Verifies:
//   - infra/compose.yaml: Keycloak services, pinned image tags, dedicated
//     Postgres, non-colliding host port, healthchecks, restart policy, volumes.
//   - infra/keycloak/realm-lorehub.json: realm, confidential OIDC client with
//     PKCE, bearer-only API client, password policy, brute-force protection,
//     session lifetimes, email-as-login, self-registration, no baked IDPs or
//     hardcoded secrets.
//   - infra/keycloak/bootstrap.sh: conditional provisioning for all four
//     providers, idempotent upsert, no secret logging.
//   - .env.example: every required variable present with empty secret defaults.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const errors = [];

function read(path) {
  return readFileSync(join(root, path), "utf8");
}

function expect(condition, message) {
  if (!condition) errors.push(message);
}

// --- compose.yaml (targeted structural checks, no YAML dependency) -----------
const compose = read("infra/compose.yaml");

expect(/image: quay\.io\/keycloak\/keycloak:26\.7\.1\b/.test(compose), "Keycloak image must be pinned to 26.7.1");
expect(/image: postgres:18\.4-alpine\b/.test(compose), "Postgres image must be pinned to 18.4-alpine");
expect(/^\s*keycloak-postgres:\s*$/m.test(compose), "compose must define a dedicated keycloak-postgres service");
expect(/^\s*keycloak:\s*$/m.test(compose), "compose must define a keycloak service");
expect(/^\s*keycloak-bootstrap:\s*$/m.test(compose), "compose must define a keycloak-bootstrap service");
expect(/keycloak-postgres-data:/.test(compose), "compose must declare a keycloak-postgres-data volume");
expect(/keycloak-data:/.test(compose), "compose must declare a keycloak-data volume");
expect(/\$\{KEYCLOAK_HOST_PORT:-8280\}:8080/.test(compose), "Keycloak host port default must be 8280");
expect(/condition: service_healthy/.test(compose), "Keycloak must wait for its Postgres healthcheck");
expect(/LOREHUB_AUTH_MODE: \$\{LOREHUB_AUTH_MODE:-interactive\}/.test(compose), "API must default to interactive auth");
expect(
  /LOREHUB_OIDC_ISSUER: \$\{LOREHUB_OIDC_ISSUER:-http:\/\/keycloak\.localhost:8280\/realms\/lorehub\}/.test(compose),
  "API must use the local Keycloak issuer",
);
expect(
  /LOREHUB_OIDC_AUDIENCE: \$\{LOREHUB_OIDC_AUDIENCE:-lorehub-api\}/.test(compose),
  "API audience must be lorehub-api",
);
expect(
  /LOREHUB_OIDC_CLIENT_ID: \$\{LOREHUB_OIDC_CLIENT_ID:-lorehub-web\}/.test(compose),
  "API client ID must be lorehub-web",
);
expect(/LOREHUB_OIDC_REDIRECT_URL:/.test(compose), "API must receive the OIDC redirect URL");
expect(/LOREHUB_PUBLIC_ORIGIN:/.test(compose), "API must receive the public origin");
expect(/LOREHUB_AUTH_SECRET:/.test(compose), "API must receive the session secret");
expect(/LOREHUB_SESSION_COOKIE_SECURE:/.test(compose), "API must receive local cookie settings");
expect(
  /keycloak\.localhost:host-gateway/.test(compose),
  "API must resolve the public Keycloak hostname through the host",
);
expect(/keycloak:\s*\n\s*condition: service_healthy/.test(compose), "API must wait for Keycloak health");
expect(!compose.includes("LOREHUB_OIDC_PUBLIC_ORIGIN"), "the obsolete OIDC public-origin variable must be absent");
expect(/--import-realm/.test(compose), "Keycloak must start with --import-realm");
expect(/--http-enabled=true/.test(compose), "Keycloak must explicitly enable HTTP for local");
expect(/--hostname-strict=false/.test(compose), "Keycloak must relax hostname strict mode for local");
expect(/--optimized/.test(compose), "Keycloak must start in optimized/production mode");
expect(/KC_BOOTSTRAP_ADMIN_PASSWORD/.test(compose), "Keycloak admin password must come from env, not the image");
expect(/KC_DB_PASSWORD:/.test(compose), "Keycloak DB password must come from the runtime environment");
expect(!/--db-password=/.test(compose), "Keycloak DB password must not be exposed in the process arguments");
expect(/LOREHUB_OIDC_CLIENT_SECRET/.test(compose), "Keycloak must receive LOREHUB_OIDC_CLIENT_SECRET for realm import");
expect(
  /KEYCLOAK_DB_PASSWORD:\?KEYCLOAK_DB_PASSWORD is required/.test(compose),
  "Keycloak DB password must be required",
);
expect(
  !/POSTGRES_DB: lorehub/.test(compose.match(/keycloak-postgres:[\s\S]*?(?=\n  [a-z]|\nvolumes:)/)?.[0] ?? ""),
  "Keycloak must not reuse the LoreHub database",
);
expect(/restart: unless-stopped/.test(compose), "Keycloak services must have a restart policy");
expect(/healthcheck:/.test(compose), "Keycloak must define a healthcheck");

// --- Dockerfile (build-time db vendor + health for --optimized) ------------
const dockerfile = read("infra/keycloak/Dockerfile");
expect(/FROM quay\.io\/keycloak\/keycloak:26\.7\.1\b/.test(dockerfile), "Dockerfile must pin base image to 26.7.1");
expect(/kc\.sh build --db=postgres/.test(dockerfile), "Dockerfile must build with --db=postgres for optimized startup");
expect(/--health-enabled=true/.test(dockerfile), "Dockerfile must enable health at build time");
const dockerfileRunLines = dockerfile
  .split("\n")
  .filter((line) => /^\s*RUN\s/.test(line))
  .join("\n");
expect(
  !/password|secret|admin/i.test(dockerfileRunLines),
  "Dockerfile RUN steps must not bake credentials into the image",
);

// --- realm JSON -------------------------------------------------------------
const realm = JSON.parse(read("infra/keycloak/realm-lorehub.json"));

expect(realm.realm === "lorehub", "realm name must be lorehub");
expect(realm.enabled === true, "realm must be enabled");
expect(realm.registrationAllowed === true, "self-registration must be allowed");
expect(realm.registrationEmailAsUsername === true, "email must be used as username");
expect(realm.loginWithEmailAllowed === true, "email login must be allowed");
expect(realm.resetPasswordAllowed === true, "password reset must be allowed");
expect(realm.verifyEmail === false, "development realm import must not require unavailable SMTP");
expect(realm.sslRequired === "external", "realm must require TLS outside local/private addresses");
expect(realm.internationalizationEnabled === true, "realm internationalization must be enabled");
expect(
  realm.defaultLocale === "en" &&
    Array.isArray(realm.supportedLocales) &&
    realm.supportedLocales.includes("en") &&
    realm.supportedLocales.includes("ja"),
  "realm must support English and Japanese",
);
expect(realm.bruteForceProtected === true, "brute-force protection must be enabled");
expect(typeof realm.passwordPolicy === "string" && realm.passwordPolicy.length > 0, "password policy must be set");
expect(/length\(12\)/.test(realm.passwordPolicy), "password policy must require at least 12 characters");
expect(/specialChars/.test(realm.passwordPolicy), "password policy must require special characters");
expect(/digits/.test(realm.passwordPolicy), "password policy must require digits");
expect(/notUsername/.test(realm.passwordPolicy), "password policy must reject the username");
expect(/notEmail/.test(realm.passwordPolicy), "password policy must reject the email");
expect(
  realm.accessTokenLifespan > 0 && realm.accessTokenLifespan <= 600,
  "access token lifespan must be <= 10 minutes",
);
expect(realm.ssoSessionIdleTimeout > 0, "session idle timeout must be set");
expect(realm.revokeRefreshToken === true, "refresh token revocation must be enabled");
expect(
  Array.isArray(realm.identityProviders) && realm.identityProviders.length === 0,
  "realm must not bake in identity providers",
);

const webClient = realm.clients.find((c) => c.clientId === "lorehub-web");
const apiClient = realm.clients.find((c) => c.clientId === "lorehub-api");
expect(webClient !== undefined, "lorehub-web client must exist");
expect(apiClient !== undefined, "lorehub-api client must exist");
if (webClient) {
  expect(webClient.publicClient === false, "lorehub-web must be confidential");
  expect(webClient.bearerOnly === false, "lorehub-web must not be bearer-only");
  expect(webClient.standardFlowEnabled === true, "lorehub-web must enable Authorization Code flow");
  expect(webClient.directAccessGrantsEnabled === false, "lorehub-web must not allow password grant");
  expect(webClient.attributes?.pkceCodeChallengeMethod === "S256", "lorehub-web must require PKCE S256");
  expect(webClient.secret === "${LOREHUB_OIDC_CLIENT_SECRET}", "lorehub-web secret must be an env placeholder");
  expect(
    Array.isArray(webClient.redirectUris) &&
      webClient.redirectUris.length === 1 &&
      webClient.redirectUris[0] === "http://localhost:3000/auth/callback",
    "lorehub-web must allow only the API callback",
  );
  expect(
    Array.isArray(webClient.webOrigins) &&
      webClient.webOrigins.length === 1 &&
      webClient.webOrigins[0] === "http://localhost:3000",
    "lorehub-web must allow only the local web origin",
  );
  expect(
    webClient.attributes?.["post.logout.redirect.uris"] === "http://localhost:3000/",
    "lorehub-web must have an exact local logout URI",
  );
  expect(!JSON.stringify(webClient).includes("/api/auth/callback/lorehub"), "NextAuth callback must be absent");
  expect(!JSON.stringify(webClient).includes('"+"'), "lorehub-web must not use permissive origins");
  const audienceMapper = webClient.protocolMappers?.find((m) => m.protocolMapper === "oidc-audience-mapper");
  expect(
    audienceMapper?.config?.["included.client.audience"] === "lorehub-api",
    "lorehub-web must add lorehub-api to token audience",
  );
}
if (apiClient) {
  expect(apiClient.bearerOnly === true, "lorehub-api must be bearer-only");
  expect(apiClient.standardFlowEnabled === false, "lorehub-api must not enable login flow");
}
const scopeNames = (realm.clientScopes ?? []).map((s) => s.name);
for (const name of ["profile", "email", "roles", "web-origins"]) {
  expect(scopeNames.includes(name), `realm must define the ${name} client scope`);
}
expect((realm.defaultDefaultClientScopes ?? []).includes("profile"), "profile must be a default client scope");

// --- bootstrap.sh -----------------------------------------------------------
const bootstrap = read("infra/keycloak/bootstrap.sh");
for (const provider of ["google", "github", "facebook", "x"]) {
  expect(
    new RegExp(`LOREHUB_IDP_${provider.toUpperCase()}_CLIENT_ID`).test(bootstrap),
    `bootstrap must read ${provider} client id`,
  );
  expect(
    new RegExp(`LOREHUB_IDP_${provider.toUpperCase()}_CLIENT_SECRET`).test(bootstrap),
    `bootstrap must read ${provider} client secret`,
  );
  expect(new RegExp(`upsert_provider ${provider}`).test(bootstrap), `bootstrap must provision ${provider}`);
}
expect(bootstrap.includes("provider_exists"), "bootstrap must check provider existence (idempotent)");
expect(bootstrap.includes("disable_provider"), "bootstrap must disable providers whose credentials were removed");
expect(bootstrap.includes("storeToken=false"), "bootstrap must not store upstream provider tokens");
expect(bootstrap.includes("trustEmail=false"), "bootstrap must not trust upstream email claims");
expect(bootstrap.includes("enabled=true"), "bootstrap must enable configured providers");
expect(bootstrap.includes("passwordHistory(3)"), "bootstrap must apply password history");
expect(bootstrap.includes("LOREHUB_VERIFY_EMAIL"), "bootstrap must configure email verification");
expect(bootstrap.includes("KEYCLOAK_SMTP_HOST"), "bootstrap must configure SMTP from environment");
expect(bootstrap.includes("production requires LOREHUB_VERIFY_EMAIL=true"), "production must require verification");
expect(bootstrap.includes("post.logout.redirect.uris"), "bootstrap must update the logout URI");
expect(bootstrap.includes('redirectUris=[\\"${REDIRECT_URL}\\"]'), "bootstrap must update only the API callback");
expect(!bootstrap.includes("offline.access"), "X must not request unnecessary offline access");
expect(bootstrap.includes("x oauth2"), "X must use the generic oauth2 provider");
expect(bootstrap.includes("https://x.com/i/oauth2/authorize"), "bootstrap must use X's real authorization endpoint");
expect(bootstrap.includes("https://api.x.com/2/oauth2/token"), "bootstrap must use X's real token endpoint");
expect(bootstrap.includes("https://api.x.com/2/users/me"), "bootstrap must use X's real userinfo endpoint");
expect(bootstrap.includes("data.id") && bootstrap.includes("data.username"), "bootstrap must map X nested data claims");
expect(!/echo .*SECRET/.test(bootstrap), "bootstrap must not echo secret values");

// --- .env.example -----------------------------------------------------------
const envExample = read(".env.example");
const requiredEnvVars = [
  "POSTGRES_PASSWORD",
  "LOREHUB_ENV",
  "LOREHUB_AUTH_MODE",
  "LOREHUB_OIDC_ISSUER",
  "LOREHUB_OIDC_AUDIENCE",
  "LOREHUB_OIDC_CLIENT_ID",
  "LOREHUB_OIDC_CLIENT_SECRET",
  "LOREHUB_OIDC_REDIRECT_URL",
  "LOREHUB_OIDC_LOGOUT_REDIRECT_URL",
  "LOREHUB_PUBLIC_ORIGIN",
  "LOREHUB_AUTH_SECRET",
  "LOREHUB_SESSION_COOKIE_SECURE",
  "KEYCLOAK_REALM",
  "KEYCLOAK_HOST_PORT",
  "KEYCLOAK_HOSTNAME",
  "KEYCLOAK_ADMIN_USERNAME",
  "KEYCLOAK_ADMIN_PASSWORD",
  "KEYCLOAK_DB_USER",
  "KEYCLOAK_DB_NAME",
  "KEYCLOAK_DB_PASSWORD",
  "LOREHUB_VERIFY_EMAIL",
  "KEYCLOAK_SMTP_HOST",
  "KEYCLOAK_SMTP_PORT",
  "KEYCLOAK_SMTP_FROM",
  "KEYCLOAK_SMTP_AUTH",
  "KEYCLOAK_SMTP_USER",
  "KEYCLOAK_SMTP_PASSWORD",
  "KEYCLOAK_SMTP_STARTTLS",
  "KEYCLOAK_SMTP_SSL",
  "LOREHUB_IDP_GOOGLE_CLIENT_ID",
  "LOREHUB_IDP_GOOGLE_CLIENT_SECRET",
  "LOREHUB_IDP_GITHUB_CLIENT_ID",
  "LOREHUB_IDP_GITHUB_CLIENT_SECRET",
  "LOREHUB_IDP_FACEBOOK_CLIENT_ID",
  "LOREHUB_IDP_FACEBOOK_CLIENT_SECRET",
  "LOREHUB_IDP_X_CLIENT_ID",
  "LOREHUB_IDP_X_CLIENT_SECRET",
];
for (const name of requiredEnvVars) {
  expect(new RegExp(`^${name}=`, "m").test(envExample), `.env.example must define ${name}`);
}
const secretEnvVars = [
  "POSTGRES_PASSWORD",
  "KEYCLOAK_ADMIN_PASSWORD",
  "KEYCLOAK_DB_PASSWORD",
  "LOREHUB_OIDC_CLIENT_SECRET",
  "LOREHUB_AUTH_SECRET",
  "KEYCLOAK_SMTP_PASSWORD",
  "LOREHUB_IDP_GOOGLE_CLIENT_SECRET",
  "LOREHUB_IDP_GITHUB_CLIENT_SECRET",
  "LOREHUB_IDP_FACEBOOK_CLIENT_SECRET",
  "LOREHUB_IDP_X_CLIENT_SECRET",
];
for (const name of secretEnvVars) {
  const line = envExample.match(new RegExp(`^${name}=(.*)$`, "m"));
  expect(line !== null && line[1] === "", `.env.example ${name} must default to empty (no committed secret)`);
}

// --- best-effort docker compose config validation --------------------------
try {
  execFileSync("docker", ["compose", "-f", "infra/compose.yaml", "config", "-q"], {
    cwd: root,
    stdio: "pipe",
    env: {
      ...process.env,
      POSTGRES_PASSWORD: "check",
      KEYCLOAK_DB_PASSWORD: "check",
      KEYCLOAK_ADMIN_PASSWORD: "check",
      LOREHUB_OIDC_CLIENT_SECRET: "check",
      LOREHUB_AUTH_SECRET: "check-check-check-check-check-check-check-check",
      LOREHUB_RUNNER_USER_ID: "00000000-0000-0000-0000-000000000001",
    },
  });
} catch (error) {
  const detail = error.stderr ? error.stderr.toString().trim() : error.message;
  errors.push(`docker compose config validation failed: ${detail}`);
}

if (errors.length > 0) {
  console.error("Keycloak infrastructure validation failed:");
  for (const error of errors) console.error(`  - ${error}`);
  process.exitCode = 1;
} else {
  console.error("Keycloak infrastructure validation passed.");
}
