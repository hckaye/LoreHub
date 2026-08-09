import assert from "node:assert/strict";
import test from "node:test";

import { validateBranchSelection, validateTitleAndBody } from "../src/lib/validation";

test("issue validation trims required titles and enforces backend limits", () => {
  assert.deepEqual(validateTitleAndBody("  ", ""), ["titleRequired"]);
  assert.deepEqual(validateTitleAndBody("A title", ""), []);
  assert.deepEqual(validateTitleAndBody("A".repeat(513), ""), ["titleTooLong"]);
  assert.deepEqual(validateTitleAndBody("A title", "A".repeat(1_000_001)), ["bodyTooLong"]);
});

test("pull request validation prevents the same source and target branch", () => {
  assert.deepEqual(validateBranchSelection("main", "main"), ["branchesSame"]);
  assert.deepEqual(validateBranchSelection("feature", "main"), []);
});
