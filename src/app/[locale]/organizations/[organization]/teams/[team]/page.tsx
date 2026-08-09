import { notFound } from "next/navigation";

import { TeamPage } from "@/components/organizations/team-page";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getTeam } from "@/lib/lorehub-api";

type TeamRouteProps = {
  params: Promise<{ locale: string; organization: string; team: string }>;
};

export const dynamic = "force-dynamic";

export default async function TeamRoute({ params }: TeamRouteProps) {
  const { locale: value, organization, team: teamSlug } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const [dictionary, session, result] = await Promise.all([
    getDictionary(value),
    getAuthSession(),
    getTeam(organization, teamSlug),
  ]);
  if (!result.ok) {
    return (
      <TeamPage dictionary={dictionary} members={[]} organizationSlug={organization} session={session} team={null} />
    );
  }
  return (
    <TeamPage
      dictionary={dictionary}
      members={result.data.members ?? []}
      organizationSlug={organization}
      session={session}
      team={result.data.team}
    />
  );
}
