export type StreamingRequestInit = RequestInit & { duplex?: "half" };

export function createUpstreamRequestInit(request: Request, headers: Headers): StreamingRequestInit {
  const init: StreamingRequestInit = {
    cache: "no-store",
    headers,
    method: request.method,
    redirect: "manual",
    signal: request.signal,
  };
  if (request.method !== "GET" && request.method !== "HEAD" && request.body) {
    init.body = request.body;
    init.duplex = "half";
  }
  return init;
}
