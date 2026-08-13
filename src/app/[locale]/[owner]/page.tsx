import { notFound } from "next/navigation";

import { UserProfilePage } from "@/components/profile/user-profile-page";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getUserProfile, getUserRepositories } from "@/lib/lorehub-api";

type PublicUserPageProps = {
  params: Promise<{ locale: string; owner: string }>;
};

export const dynamic = "force-dynamic";

export default async function PublicUserPage({ params }: PublicUserPageProps) {
  const { locale: value, owner } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const [dictionary, profile, repositories] = await Promise.all([
    getDictionary(value),
    getUserProfile(owner),
    getUserRepositories(owner),
  ]);
  if (!profile.ok && profile.reason === "not-found") {
    notFound();
  }
  return (
    <UserProfilePage
      dictionary={dictionary}
      locale={value}
      profile={profile.ok ? profile.data : null}
      repositories={repositories.ok ? repositories.data : null}
      unavailable={!profile.ok}
    />
  );
}
