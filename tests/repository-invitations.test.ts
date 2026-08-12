import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  classifyRepositoryInvitationStatus,
  createRepositoryInvitation,
  parseRepositoryInvitationPage,
  repositoryInvitationAccountPath,
  repositoryInvitationAdminPath,
  respondRepositoryInvitation,
  revokeRepositoryCollaborator,
  revokeRepositoryInvitation,
  updateRepositoryCollaboratorRole,
} from "../src/lib/repository-invitations";

const invitationID = "00000000-0000-4000-8000-000000000001";
const organizationID = "00000000-0000-4000-8000-000000000002";
const repositoryID = "00000000-0000-4000-8000-000000000003";
const inviteeID = "00000000-0000-4000-8000-000000000004";
const inviterID = "00000000-0000-4000-8000-000000000005";

test("repository invitation pages are parsed strictly", () => {
  const page = invitationPage();
  assert.deepEqual(parseRepositoryInvitationPage(page, 1), page);
  assert.equal(parseRepositoryInvitationPage({ ...page, extra: true }, 1), null);
  assert.equal(parseRepositoryInvitationPage(page, 2), null);
  assert.equal(parseRepositoryInvitationPage({ ...page, total: 2 }, 1), null);
  assert.equal(
    parseRepositoryInvitationPage({ ...page, invitations: [{ ...page.invitations[0], role: "owner" }] }, 1),
    null,
  );
});

test("repository invitation paths encode repository names and pagination", () => {
  assert.equal(
    repositoryInvitationAdminPath("acme studio", "game/client", 3),
    "/api/v1/repositories/acme%20studio/game%2Fclient/invitations?page=3&per_page=20",
  );
  assert.equal(repositoryInvitationAccountPath(2), "/api/v1/account/repository-invitations?page=2&per_page=20");
});

test("repository invitation mutations use CSRF and exact routes", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ input: string; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ input: String(input), init });
    if (init?.method === "DELETE" && String(input).includes("/invitations/")) {
      return new Response(null, { status: 204 });
    }
    if (String(input).includes("/collaborators/")) {
      return Response.json(collaborator());
    }
    return Response.json(invitationPage().invitations[0]);
  };
  try {
    assert.equal((await createRepositoryInvitation("acme", "game", "alice", "write", "csrf-token")).ok, true);
    assert.equal((await respondRepositoryInvitation(invitationID, "accept", "csrf-token")).ok, true);
    assert.equal((await revokeRepositoryInvitation("acme", "game", invitationID, "csrf-token")).ok, true);
    assert.equal((await updateRepositoryCollaboratorRole("acme", "game", "alice", "admin", "csrf-token")).ok, true);
    assert.equal((await revokeRepositoryCollaborator("acme", "game", "alice", "csrf-token")).ok, true);
  } finally {
    globalThis.fetch = originalFetch;
  }

  assert.deepEqual(
    requests.map((request) => request.init?.method),
    ["POST", "POST", "DELETE", "PUT", "DELETE"],
  );
  for (const request of requests) {
    assert.equal(new Headers(request.init?.headers).get("X-CSRF-Token"), "csrf-token");
  }
  assert.equal(requests[1]?.input, `/api/v1/account/repository-invitations/${invitationID}/accept`);
  assert.equal(requests[2]?.input, `/api/v1/repositories/acme/game/invitations/${invitationID}`);
});

test("repository invitation status errors remain distinct", () => {
  assert.equal(classifyRepositoryInvitationStatus(401), "unauthorized");
  assert.equal(classifyRepositoryInvitationStatus(403), "forbidden");
  assert.equal(classifyRepositoryInvitationStatus(404), "not-found");
  assert.equal(classifyRepositoryInvitationStatus(409), "conflict");
  assert.equal(classifyRepositoryInvitationStatus(422), "invalid");
  assert.equal(classifyRepositoryInvitationStatus(503), "unavailable");
});

test("account and repository settings mount the invitation components", async () => {
  const [accountPage, repositoryPage, accessSettings] = await Promise.all([
    readFile("src/app/[locale]/settings/page.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/settings/page.tsx", "utf8"),
    readFile("src/components/repositories/repository-access-settings.tsx", "utf8"),
  ]);
  assert.match(accountPage, /RepositoryInvitationSettings/u);
  assert.match(repositoryPage, /locale=\{locale\}/u);
  assert.match(accessSettings, /RepositoryCollaboratorSettings/u);
  assert.doesNotMatch(accessSettings, /saveCollaborator/u);
});

function invitationPage() {
  return {
    invitations: [
      {
        id: invitationID,
        organizationId: organizationID,
        repositoryId: repositoryID,
        owner: "acme",
        repository: "game",
        repositoryDisplayName: "Game",
        inviteeUserId: inviteeID,
        inviteeUsername: "alice",
        inviteeDisplayName: "Alice",
        invitedByUserId: inviterID,
        invitedByUsername: "owner",
        invitedByDisplayName: "Owner",
        role: "write",
        status: "pending",
        expiresAt: "2026-08-19T10:00:00Z",
        respondedAt: null,
        createdAt: "2026-08-12T10:00:00Z",
        updatedAt: "2026-08-12T10:00:00Z",
      },
    ],
    total: 1,
    page: 1,
    perPage: 20,
  } as const;
}

function collaborator() {
  return {
    userId: inviteeID,
    username: "alice",
    displayName: "Alice",
    role: "admin",
    active: true,
    source: "direct",
  };
}
