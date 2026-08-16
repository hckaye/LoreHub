import { NextRequest, NextResponse } from "next/server";

import { defaultLocale } from "@/i18n/config";
import { safeReturnTo } from "@/lib/routes";

export const dynamic = "force-dynamic";

// The Go API redirects bare /auth/login here when no external OIDC provider is
// configured. This handler picks a locale and forwards to the branded page.
export function GET(request: NextRequest) {
  const acceptedLanguages = request.headers.get("accept-language")?.toLowerCase() ?? "";
  const locale = acceptedLanguages.includes("ja") ? "ja" : defaultLocale;
  const register = request.nextUrl.searchParams.get("prompt") === "create";
  const returnTo = safeReturnTo(request.nextUrl.searchParams.get("return_to"));
  const url = request.nextUrl.clone();
  url.pathname = `/${locale}/auth/${register ? "register" : "login"}`;
  url.search = `?return_to=${encodeURIComponent(returnTo)}`;
  return NextResponse.redirect(url);
}
