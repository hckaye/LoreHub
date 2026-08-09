import { readdir, readFile } from "node:fs/promises";
import { extname, join } from "node:path";

const root = new URL("../", import.meta.url);
const ignoredDirectories = new Set([".git", ".next", ".cache", ".vinext", "coverage", "dist", "node_modules", "out"]);
const ignoredFiles = new Set(["go.sum", "package-lock.json"]);
const checkedExtensions = new Set([
  "",
  ".css",
  ".go",
  ".html",
  ".js",
  ".json",
  ".md",
  ".mjs",
  ".proto",
  ".rs",
  ".sh",
  ".sql",
  ".toml",
  ".ts",
  ".tsx",
  ".yaml",
  ".yml",
]);

const violations = [];
await visit(root);

if (violations.length > 0) {
  for (const violation of violations) {
    console.error(violation);
  }
  process.exitCode = 1;
}

async function visit(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  for (const entry of entries) {
    if (entry.isDirectory() && ignoredDirectories.has(entry.name)) {
      continue;
    }
    const path = new URL(entry.name + (entry.isDirectory() ? "/" : ""), directory);
    if (entry.isDirectory()) {
      await visit(path);
      continue;
    }
    if (ignoredFiles.has(entry.name) || !checkedExtensions.has(extname(entry.name))) {
      continue;
    }
    await check(path);
  }
}

async function check(url) {
  const bytes = await readFile(url);
  if (extname(url.pathname) === "" && bytes.includes(0)) {
    return;
  }
  const contents = bytes.toString("utf8");
  const lines = contents.split(/\r?\n/);
  const displayPath = join(...url.pathname.split("/").slice(-6));
  if (lines.length >= 1_000) {
    violations.push(`${displayPath}: ${lines.length} lines; files must stay below 1000 lines`);
  }
  lines.forEach((line, index) => {
    if ([...line].length > 120) {
      violations.push(`${displayPath}:${index + 1}: line is longer than 120 characters`);
    }
  });
}
