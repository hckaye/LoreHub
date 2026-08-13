import { FilterTabs } from "@/components/ui/filter-tabs";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { repositoryPath } from "@/lib/routes";

export type RepositorySettingsTab = "general" | "runners";

export type OrganizationSettingsTab = "general" | "runners" | "loreServers" | "auditLog";

type RepositorySettingsTabsProps = {
  active: RepositorySettingsTab;
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
};

type OrganizationSettingsTabsProps = {
  active: OrganizationSettingsTab;
  dictionary: Dictionary;
  locale: Locale;
  organization: string;
};

export function RepositorySettingsTabs({ active, dictionary, locale, owner, repository }: RepositorySettingsTabsProps) {
  const base = repositoryPath(locale, owner, repository, "settings");
  const copy = dictionary.settingsNav;
  return (
    <FilterTabs
      label={copy.label}
      tabs={[
        { href: base, label: copy.general, active: active === "general" },
        { href: `${base}/runners`, label: copy.runners, active: active === "runners" },
      ]}
    />
  );
}

export function OrganizationSettingsTabs({ active, dictionary, locale, organization }: OrganizationSettingsTabsProps) {
  const base = organizationSettingsPath(locale, organization);
  const copy = dictionary.settingsNav;
  return (
    <FilterTabs
      label={copy.label}
      tabs={[
        { href: base, label: copy.general, active: active === "general" },
        { href: `${base}/runners`, label: copy.runners, active: active === "runners" },
        { href: `${base}/lore-servers`, label: copy.loreServers, active: active === "loreServers" },
        { href: `${base}/audit-log`, label: copy.auditLog, active: active === "auditLog" },
      ]}
    />
  );
}

export function organizationSettingsPath(locale: Locale, organization: string): string {
  return `/${locale}/organizations/${encodeURIComponent(organization)}/settings`;
}
