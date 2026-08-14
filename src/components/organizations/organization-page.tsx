import { RepositoryRows } from "@/components/repositories/repository-rows";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, OrganizationView, Repository } from "@/lib/api-types";

import { OrganizationProfile } from "./organization-profile";

type OrganizationPageProps = {
  dictionary: Dictionary;
  locale: Locale;
  organization: OrganizationView | null;
  repositories: Repository[] | null;
  session: AuthSession;
};

export function OrganizationPage({ dictionary, locale, organization, repositories, session }: OrganizationPageProps) {
  if (!organization) {
    return (
      <OrganizationProfile
        dictionary={dictionary}
        locale={locale}
        organization={null}
        session={session}
        tab="repositories"
      />
    );
  }
  return (
    <OrganizationProfile
      dictionary={dictionary}
      locale={locale}
      organization={organization}
      session={session}
      tab="repositories"
    >
      <RepositoryRows
        dictionary={dictionary}
        emptyBody={dictionary.organizationPage.noRepositoriesBody}
        emptyTitle={dictionary.organizationPage.noRepositories}
        locale={locale}
        repositories={repositories}
        unavailable={!repositories}
      />
    </OrganizationProfile>
  );
}
