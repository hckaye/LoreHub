import "server-only";

import { NextRequest } from "next/server";

import { createUpstreamRequestInit } from "./api-proxy-request";

const hopByHopHeaders = [
  "connection",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
];

export async function proxyAPIRequest(
  request: NextRequest,
  prefix: "api" | "auth",
  segments: string[],
): Promise<Response> {
  const apiOrigin = process.env.LOREHUB_API_URL;
  if (!apiOrigin) {
    return proxyError("LoreHub API origin is not configured", 503);
  }

  let upstreamURL: URL;
  try {
    upstreamURL = new URL(buildUpstreamPath(prefix, segments, request.nextUrl.search), apiOrigin);
  } catch {
    return proxyError("LoreHub API origin is invalid", 503);
  }

  const headers = new Headers(request.headers);
  for (const name of hopByHopHeaders) {
    headers.delete(name);
  }
  headers.delete("host");
  headers.set("x-forwarded-host", request.headers.get("host") ?? request.nextUrl.host);
  headers.set("x-forwarded-proto", request.nextUrl.protocol.replace(":", ""));

  const init = createUpstreamRequestInit(request, headers);

  try {
    const upstream = await fetch(upstreamURL, init);
    const responseHeaders = new Headers(upstream.headers);
    for (const name of hopByHopHeaders) {
      responseHeaders.delete(name);
    }
    return new Response(upstream.body, {
      headers: responseHeaders,
      status: upstream.status,
      statusText: upstream.statusText,
    });
  } catch {
    return proxyError("LoreHub API is unavailable", 502);
  }
}

function buildUpstreamPath(prefix: string, segments: string[], search: string): string {
  const suffix = segments.map(encodeURIComponent).join("/");
  return `/${prefix}/${suffix}${search}`;
}

function proxyError(message: string, status: number): Response {
  return Response.json({ error: message }, { status });
}
