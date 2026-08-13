import type { Metadata } from "next";
import { notFound } from "next/navigation";
import type { ReactNode } from "react";

import { AuthNotice } from "@/components/auth/auth-notice";
import { SiteFooter } from "@/components/layout/site-footer";
import { SiteHeader } from "@/components/layout/site-header";
import { getDictionary } from "@/i18n";
import { isLocale, locales, type Locale } from "@/i18n/config";
import { getAuthSession, getUnreadNotificationCount } from "@/lib/auth-api";

import "./globals.css";

type LayoutProps = {
  children: ReactNode;
  params: Promise<{ locale: string }>;
};

export function generateStaticParams() {
  return locales.map((locale) => ({ locale }));
}

export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: LayoutProps): Promise<Metadata> {
  const { locale: value } = await params;
  const locale: Locale = isLocale(value) ? value : "en";
  const dictionary = await getDictionary(locale);
  return {
    title: dictionary.metadata.title,
    description: dictionary.metadata.description,
  };
}

export default async function LocaleLayout({ children, params }: LayoutProps) {
  const { locale: value } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const [dictionary, session] = await Promise.all([getDictionary(value), getAuthSession()]);
  const unreadNotifications =
    session.status === "authenticated" ? await getUnreadNotificationCount() : { ok: false as const };
  return (
    <html lang={value}>
      <body>
        <a className="skip-link" href="#main-content">
          {dictionary.common.skipToContent}
        </a>
        <div className="site-shell">
          <SiteHeader
            dictionary={dictionary}
            locale={value}
            session={session}
            unreadNotifications={unreadNotifications.ok ? unreadNotifications.data : 0}
          />
          <AuthNotice dictionary={dictionary} session={session} />
          <main className="site-main" id="main-content">
            {children}
          </main>
          <SiteFooter dictionary={dictionary} />
        </div>
      </body>
    </html>
  );
}
