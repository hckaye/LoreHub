import { notFound } from "next/navigation";

import { OrganizationTeamsPage } from "@/components/organizations/organization-teams-page";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getOrganization, getTeams } from "@/lib/lorehub-api";

type OrganizationTeamsRouteProps = {
  params: Promise<{ locale: string; organization: string }>;
};

export const dynamic = "force-dynamic";

export default async function OrganizationTeamsRoute({ params }: OrganizationTeamsRouteProps) {
  const { locale: value, organization: slug } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const [dictionary, session, organization, teams] = await Promise.all([
    getDictionary(value),
    getAuthSession(),
    getOrganization(slug),
    getTeams(slug),
  ]);
  return (
    <OrganizationTeamsPage
      dictionary={dictionary}
      locale={value}
      organization={organization.ok ? organization.data : null}
      session={session}
      teams={teams.ok ? teams.data : null}
    />
  );
}
