import { redirect } from "next/navigation";

import { isLocale } from "@/i18n/config";
import { accountSettingsPath } from "@/lib/routes";

type AccountSettingsIndexPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function AccountSettingsIndexPage({ params }: AccountSettingsIndexPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  redirect(accountSettingsPath(locale, "profile"));
}
