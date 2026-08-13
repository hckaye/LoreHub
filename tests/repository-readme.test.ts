import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import type { TreeEntry } from "../src/lib/api-types";
import {
  findRepositoryReadme,
  transformRepositoryReadmeURL,
  type RepositoryReadmeContext,
} from "../src/lib/repository-readme";

const entries: TreeEntry[] = [
  { name: "README", path: "docs/README", kind: "file", mode: 0, size: 10 },
  { name: "ReadMe.MD", path: "docs/ReadMe.MD", kind: "file", mode: 0, size: 20 },
  { name: "guides", path: "docs/guides", kind: "directory", mode: 0, size: 0 },
];

const context: RepositoryReadmeContext = {
  locale: "en",
  owner: "acme studio",
  repository: "lore/client",
  revision: "revision-1",
  readmePath: "docs/ReadMe.MD",
  entries,
};

test("README selection is case-insensitive and prefers Markdown", () => {
  assert.equal(findRepositoryReadme(entries)?.path, "docs/ReadMe.MD");
  assert.equal(findRepositoryReadme([{ ...entries[0], kind: "link" }]), undefined);
});

test("README links stay on the pinned Lore revision", () => {
  const directories = new Set(["docs/guides"]);
  assert.equal(
    transformRepositoryReadmeURL("guides", "href", "a", context, directories),
    "/en/acme%20studio/lore%2Fclient?revision=revision-1&path=docs%2Fguides",
  );
  assert.equal(
    transformRepositoryReadmeURL("guides/setup.md#install", "href", "a", context, directories),
    "/en/acme%20studio/lore%2Fclient/blob?revision=revision-1&path=docs%2Fguides%2Fsetup.md#install",
  );
  assert.equal(
    transformRepositoryReadmeURL("../assets/logo.png", "src", "img", context),
    "/api/v1/repositories/acme%20studio/lore%2Fclient/raw?revision=revision-1&path=assets%2Flogo.png",
  );
});

test("README links reject traversal, encoded separators, and active content", () => {
  assert.equal(transformRepositoryReadmeURL("../../secret", "href", "a", context), "");
  assert.equal(transformRepositoryReadmeURL("asset%2Fsecret", "src", "img", context), "");
  assert.equal(transformRepositoryReadmeURL("javascript:alert(1)", "href", "a", context), "");
  assert.equal(
    transformRepositoryReadmeURL("https://example.com/docs", "href", "a", context),
    "https://example.com/docs",
  );
});

test("repository pages fetch README content and use safe GFM rendering", async () => {
  const [page, blob, readme, markdown] = await Promise.all([
    readFile("src/app/[locale]/[owner]/[repository]/page.tsx", "utf8"),
    readFile("src/components/repositories/blob-view-file.tsx", "utf8"),
    readFile("src/components/repositories/repository-readme.tsx", "utf8"),
    readFile("src/components/wiki/markdown-content.tsx", "utf8"),
  ]);
  assert.match(page, /findRepositoryReadme/);
  assert.match(page, /getLoreFile/);
  assert.match(blob, /MarkdownContent/);
  assert.match(blob, /createRepositoryReadmeURLTransform/);
  assert.match(readme, /createRepositoryReadmeURLTransform/);
  assert.match(markdown, /remarkGfm/);
  assert.match(markdown, /skipHtml/);
  assert.doesNotMatch(markdown, /rehypeRaw|dangerouslySetInnerHTML/);
});
