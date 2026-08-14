import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const popupComponents = [
  "src/components/layout/account-menu.tsx",
  "src/components/layout/create-menu.tsx",
  "src/components/repositories/branch-selector.tsx",
  "src/components/repositories/code-browser.tsx",
  "src/components/repositories/issue-sidebar-menu.tsx",
  "src/components/repositories/pending-review-bar.tsx",
  "src/components/repositories/project-card.tsx",
  "src/components/repositories/project-column.tsx",
  "src/components/repositories/pull-request-metadata.tsx",
  "src/components/repositories/reaction-bar.tsx",
  "src/components/repositories/repository-header.tsx",
  "src/components/repositories/work-item-list-toolbar.tsx",
];

test("the shared popup closes on outside pointers, Escape, navigation, and other popups", async () => {
  const source = await readFile("src/components/ui/popup-menu.tsx", "utf8");
  assert.match(source, /document\.addEventListener\("pointerdown", handlePointerDown\)/);
  assert.match(source, /document\.addEventListener\("keydown", handleKeyDown\)/);
  assert.match(source, /containersRef\.current\.some\(\(container\) => container\.current\?\.contains\(target\)\)/);
  assert.match(source, /for \(const closeOther of openPopups\)/);
  assert.match(source, /openPopups\.add\(close\)/);
  assert.match(source, /openPopups\.delete\(close\)/);
  assert.match(source, /const open = openRoute === pathname/);
  assert.match(source, /document\.removeEventListener\("pointerdown", handlePointerDown\)/);
  assert.match(source, /document\.removeEventListener\("keydown", handleKeyDown\)/);
});

test("panels render in the document body so that no ancestor can clip them", async () => {
  const source = await readFile("src/components/ui/popup-menu.tsx", "utf8");
  assert.match(source, /createPortal\(panel, document\.body\)/);
  assert.match(source, /style=\{\{ position: "fixed", zIndex: panelLayer, \.\.\.position \}\}/);
  assert.match(source, /window\.addEventListener\("resize", follow\)/);
  assert.match(source, /window\.addEventListener\("scroll", follow, true\)/);
  assert.match(source, /window\.removeEventListener\("resize", follow\)/);
  assert.match(source, /window\.removeEventListener\("scroll", follow, true\)/);
});

test("the shared popup animates when it opens and honours reduced motion", async () => {
  const styles = await readFile("src/components/ui/popup-menu.module.css", "utf8");
  assert.match(styles, /animation: open 130ms/);
  assert.match(styles, /@keyframes open/);
  assert.match(styles, /\.panel\[data-align="start"\] \{\s*transform-origin: top left;/);
  assert.match(styles, /\.panel\[data-align="end"\] \{\s*transform-origin: top right;/);
  assert.match(styles, /@media \(prefers-reduced-motion: reduce\)/);
});

test("every popup menu uses the shared popup instead of its own open state", async () => {
  for (const path of popupComponents) {
    const source = await readFile(path, "utf8");
    assert.match(source, /PopupMenu/, `${path} should render popups with PopupMenu`);
    assert.doesNotMatch(source, /<summary/, `${path} should not open popups with a details element`);
  }
});

test("the mobile navigation drawer closes on an outside pointer as well", async () => {
  const source = await readFile("src/components/layout/site-header.tsx", "utf8");
  assert.match(source, /useDismissOnOutsideInteraction\(\{/);
  assert.match(source, /containers: \[navigationRef, navigationToggleRef\]/);
});
