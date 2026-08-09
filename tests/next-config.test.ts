import assert from "node:assert/strict";
import test from "node:test";

import nextConfig from "../next.config";

test("Next development does not generate agent rule files", () => {
  assert.equal(nextConfig.agentRules, false);
});
