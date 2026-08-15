import { notFound } from "next/navigation";

import { AuthPage } from "@/components/auth/auth-page";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthProviders } from "@/lib/auth-api";
import { safeReturnTo } from "@/lib/routes";

type LoginPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ return_to?: string }>;
};

export const dynamic = "force-dynamic";

export default async function LoginPage({ params, searchParams }: LoginPageProps) {
  const { locale: value } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const [{ return_to: returnTo }, dictionary, providerResult] = await Promise.all([
    searchParams,
    getDictionary(value),
    getAuthProviders(),
  ]);
  return (
    <AuthPage
      dictionary={dictionary}
      locale={value}
      directory={providerResult.ok ? providerResult.data : null}
      register={false}
      returnTo={safeReturnTo(returnTo)}
    />
  );
}
