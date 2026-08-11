import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import test from "node:test";

async function markdownFiles(directory: string): Promise<string[]> {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(
    entries.map(async (entry) => {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) return markdownFiles(path);
      return entry.isFile() && entry.name.endsWith(".md") ? [path] : [];
    }),
  );
  return nested.flat().sort();
}

test("public documentation keeps English and Japanese files paired", async () => {
  const paths = await markdownFiles("docs");
  const pathSet = new Set(paths);
  for (const path of paths) {
    if (path.endsWith(".ja.md")) {
      assert.ok(pathSet.has(path.replace(/\.ja\.md$/, ".md")), `missing English document for ${path}`);
      continue;
    }
    const japanesePath = path.replace(/\.md$/, ".ja.md");
    assert.ok(pathSet.has(japanesePath), `missing Japanese document for ${path}`);
    const name = path.split("/").at(-1);
    const japaneseName = japanesePath.split("/").at(-1);
    const [english, japanese] = await Promise.all([readFile(path, "utf8"), readFile(japanesePath, "utf8")]);
    const languageLinks = `[English](${name}) | [日本語](${japaneseName})`;
    assert.match(english, new RegExp(languageLinks.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
    assert.match(japanese, new RegExp(languageLinks.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
  }
});

test("README locale links point to matching documentation languages", async () => {
  const [english, japanese] = await Promise.all([readFile("README.md", "utf8"), readFile("README.ja.md", "utf8")]);
  assert.doesNotMatch(english, /docs\/.+\.ja\.md/);
  assert.doesNotMatch(japanese, /\]\(docs\/(?![^)]+\.ja\.md\))/);
});
