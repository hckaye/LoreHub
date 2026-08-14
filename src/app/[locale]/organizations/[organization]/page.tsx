import { notFound } from "next/navigation";

import { OrganizationPage } from "@/components/organizations/organization-page";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getOrganization, getOrganizationRepositories } from "@/lib/lorehub-api";

type OrganizationRouteProps = {
  params: Promise<{ locale: string; organization: string }>;
};

export const dynamic = "force-dynamic";

export default async function OrganizationRoute({ params }: OrganizationRouteProps) {
  const { locale: value, organization: slug } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const [dictionary, session, organization, repositories] = await Promise.all([
    getDictionary(value),
    getAuthSession(),
    getOrganization(slug),
    getOrganizationRepositories(slug),
  ]);
  return (
    <OrganizationPage
      dictionary={dictionary}
      locale={value}
      organization={organization.ok ? organization.data : null}
      repositories={repositories.ok ? repositories.data : null}
      session={session}
    />
  );
}
