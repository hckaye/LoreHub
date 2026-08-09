import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import { notFound } from "next/navigation";
import type { ReactNode } from "react";

import { SiteFooter } from "@/components/layout/site-footer";
import { SiteHeader } from "@/components/layout/site-header";
import { getDictionary } from "@/i18n";
import { isLocale, locales, type Locale } from "@/i18n/config";

import "./globals.css";

const geistSans = Geist({
  variable: "--font-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-mono",
  subsets: ["latin"],
});

type LayoutProps = {
  children: ReactNode;
  params: Promise<{ locale: string }>;
};

export function generateStaticParams() {
  return locales.map((locale) => ({ locale }));
}

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
  const dictionary = await getDictionary(value);
  return (
    <html lang={value}>
      <body className={`${geistSans.variable} ${geistMono.variable}`}>
        <div className="site-shell">
          <SiteHeader locale={value} dictionary={dictionary} />
          <main className="site-main">{children}</main>
          <SiteFooter dictionary={dictionary} />
        </div>
      </body>
    </html>
  );
}
