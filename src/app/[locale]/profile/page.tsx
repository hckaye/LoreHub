import { AuthRequired } from "@/components/auth/auth-required";
import { UserProfilePage } from "@/components/profile/user-profile-page";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getUserProfile, getUserRepositories } from "@/lib/lorehub-api";

type ProfilePageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function ProfilePage({ params }: ProfilePageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  if (session.status !== "authenticated") {
    return <AuthRequired dictionary={dictionary} returnTo={`/${locale}/profile`} session={session} />;
  }
  const [profile, repositories] = await Promise.all([
    getUserProfile(session.user.username),
    getUserRepositories(session.user.username),
  ]);
  return (
    <UserProfilePage
      dictionary={dictionary}
      locale={locale}
      profile={profile.ok ? profile.data : null}
      repositories={repositories.ok ? repositories.data : null}
    />
  );
}
