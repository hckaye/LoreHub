import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { createLabel, deleteLabel, updateLabel } from "../src/lib/label-client";

test("label mutations encode paths, normalize colors, and preserve CSRF", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ input, init });
    if (init?.method === "DELETE") return new Response(null, { status: 204 });
    return Response.json({ id: "label-1", name: "bug", description: "", color: "0969da" });
  };
  try {
    await createLabel("Epic Games", "Lore Hub", { name: "bug", description: "", color: "#0969da" }, "csrf");
    await updateLabel(
      "Epic Games",
      "Lore Hub",
      "label/id",
      {
        name: "defect",
        description: "Confirmed defect",
        color: "#d1242f",
      },
      "csrf",
    );
    await deleteLabel("Epic Games", "Lore Hub", "label/id", "csrf");
  } finally {
    globalThis.fetch = originalFetch;
  }

  assert.deepEqual(
    requests.map((request) => request.init?.method),
    ["POST", "PATCH", "DELETE"],
  );
  assert.equal(String(requests[0].input), "/api/v1/repositories/Epic%20Games/Lore%20Hub/labels");
  assert.equal(String(requests[1].input), "/api/v1/repositories/Epic%20Games/Lore%20Hub/labels/label%2Fid");
  assert.equal(new Headers(requests[2].init?.headers).get("X-CSRF-Token"), "csrf");
  assert.equal(JSON.parse(String(requests[0].init?.body)).color, "0969da");
  assert.equal(JSON.parse(String(requests[1].init?.body)).color, "d1242f");
});

test("repository labels page uses the production API and permission capability", async () => {
  const [page, manager, issuePage, api, apiTypes, handler] = await Promise.all([
    readFile("src/app/[locale]/[owner]/[repository]/labels/page.tsx", "utf8"),
    readFile("src/components/repositories/label-manager.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/issues/page.tsx", "utf8"),
    readFile("src/lib/lorehub-api.ts", "utf8"),
    readFile("src/lib/api-types.ts", "utf8"),
    readFile("services/api/internal/collab/label_handlers.go", "utf8"),
  ]);
  assert.match(page, /getLabelPage/);
  assert.match(page, /<LabelManager/);
  assert.match(manager, /data\.viewerCanWrite/);
  assert.match(manager, /maxLength=\{128\}/);
  assert.match(manager, /maxLength=\{10_000\}/);
  assert.match(issuePage, /repositoryPath\(locale, owner, repository, "labels"\)/);
  assert.match(api, /Promise<APIResult<LabelPage>>/);
  assert.match(apiTypes, /viewerCanWrite: boolean/);
  assert.match(handler, /RepositoryPermission|api\.permission/);
  assert.match(handler, /ViewerCanWrite/);
});
