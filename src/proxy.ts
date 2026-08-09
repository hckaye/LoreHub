import { NextRequest, NextResponse } from "next/server";

import { defaultLocale, isLocale } from "./i18n/config";

export function proxy(request: NextRequest) {
  const firstSegment = request.nextUrl.pathname.split("/")[1];
  if (isLocale(firstSegment)) {
    return NextResponse.next();
  }

  const acceptedLanguages = request.headers.get("accept-language")?.toLowerCase() ?? "";
  const locale = acceptedLanguages.includes("ja") ? "ja" : defaultLocale;
  const url = request.nextUrl.clone();
  url.pathname = `/${locale}${request.nextUrl.pathname}`;
  return NextResponse.redirect(url);
}

export const config = {
  matcher: ["/((?!api|auth|_next/static|_next/image|favicon.ico|.*\\..*).*)"],
};
