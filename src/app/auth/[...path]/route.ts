import { NextRequest } from "next/server";

import { proxyAPIRequest } from "@/lib/api-proxy";

type ProxyRouteContext = {
  params: Promise<{ path: string[] }>;
};

async function handler(request: NextRequest, context: ProxyRouteContext) {
  const { path } = await context.params;
  return proxyAPIRequest(request, "auth", path);
}

export {
  handler as DELETE,
  handler as GET,
  handler as HEAD,
  handler as OPTIONS,
  handler as PATCH,
  handler as POST,
  handler as PUT,
};
