import assert from "node:assert/strict";
import test from "node:test";

import { createUpstreamRequestInit } from "../src/lib/api-proxy-request";

test("API proxy forwards a request body as a stream and propagates cancellation", () => {
  const request = new Request("http://localhost/api", { method: "POST", body: "payload" });
  const init = createUpstreamRequestInit(request, new Headers());

  assert.equal(init.body, request.body);
  assert.equal(init.duplex, "half");
  assert.equal(init.signal, request.signal);
});

test("API proxy does not attach a body to GET requests", () => {
  const request = new Request("http://localhost/api");
  const init = createUpstreamRequestInit(request, new Headers());

  assert.equal(init.body, undefined);
  assert.equal(init.duplex, undefined);
});
