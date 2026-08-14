import { notFound } from "next/navigation";

import EntitlementSettingsPage from "../_pages/entitlements";
import NotificationSettingsPage from "../_pages/notifications";
import ProfileSettingsPage from "../_pages/profile";
import RepositoryInvitationSettingsPage from "../_pages/repositories";
import PersonalAccessTokenListPage from "../_pages/tokens";
import NewPersonalAccessTokenPage from "../_pages/tokens-new";

const pages = {
  profile: ProfileSettingsPage,
  notifications: NotificationSettingsPage,
  repositories: RepositoryInvitationSettingsPage,
  tokens: PersonalAccessTokenListPage,
  "tokens/new": NewPersonalAccessTokenPage,
  entitlements: EntitlementSettingsPage,
} as const;

type AccountSettingsSectionPageProps = {
  params: Promise<{ locale: string; section: string[] }>;
};

export const dynamic = "force-dynamic";

export default async function AccountSettingsSectionPage({ params }: AccountSettingsSectionPageProps) {
  const { locale, section } = await params;
  const Page = pages[section.join("/") as keyof typeof pages];
  if (!Page) notFound();
  return <Page params={Promise.resolve({ locale })} />;
}
