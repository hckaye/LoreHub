import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("organization audit log keeps owner authorization and tenant filtering in the data store", async () => {
  const [store, handler, migration] = await Promise.all([
    readFile("services/api/internal/platform/audit_log_store.go", "utf8"),
    readFile("services/api/internal/httpapi/audit_log_http.go", "utf8"),
    readFile("services/api/migrations/000030_audit_log_read_model.sql", "utf8"),
  ]);
  assert.match(store, /role != "owner"/);
  assert.match(store, /event\.organization_id = \$1/);
  assert.match(store, /event\.occurred_at, event\.id/);
  assert.match(store, /redactAuditDetails/);
  assert.match(handler, /private, no-store/);
  assert.match(handler, /ErrInvalidAuditCursor/);
  assert.match(migration, /organization_id, occurred_at DESC, id DESC/);
});

test("organization audit log UI uses the production API and paired locale copy", async () => {
  const [page, component, api, english, japanese] = await Promise.all([
    readFile("src/app/[locale]/organizations/[organization]/settings/audit-log/page.tsx", "utf8"),
    readFile("src/components/organizations/organization-audit-log.tsx", "utf8"),
    readFile("src/lib/lorehub-api.ts", "utf8"),
    readFile("src/i18n/dictionaries/en.ts", "utf8"),
    readFile("src/i18n/dictionaries/ja.ts", "utf8"),
  ]);
  assert.match(page, /getOrganizationAuditLog/);
  assert.match(component, /role="search"/);
  assert.match(component, /dateTime=\{event\.occurredAt\}/);
  assert.match(api, /organizations\/\$\{encodeURIComponent\(slug\)\}\/audit-log/);
  assert.match(english, /auditLog: auditLog\.en/);
  assert.match(japanese, /auditLog: auditLog\.ja/);
});
