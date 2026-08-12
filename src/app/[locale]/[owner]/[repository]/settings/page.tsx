import { Archive, LockKeyhole, ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { ActionsContextSettings } from "@/components/actions/actions-context-settings";
import { ActionsEnvironmentSettings } from "@/components/actions/actions-environment-settings";
import { AuthRequired } from "@/components/auth/auth-required";
import { DiscussionCategorySettings } from "@/components/discussions/discussion-category-settings";
import { RepositoryAccessSettings } from "@/components/repositories/repository-access-settings";
import { RepositoryDeleteSettings } from "@/components/repositories/repository-delete-settings";
import { RepositoryLifecycleSettings } from "@/components/repositories/repository-lifecycle-settings";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { RepositorySettingsForm } from "@/components/repositories/repository-settings-form";
import { EmptyState } from "@/components/ui/empty-state";
import { RepositoryWebhookSettings } from "@/components/webhooks/repository-webhook-settings";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import type { APIResult, AuthSession, DiscussionCategoriesPage } from "@/lib/api-types";
import { getAuthSession } from "@/lib/auth-api";
import {
  getActionsEnvironments,
  getDiscussionCategories,
  getOrganization,
  getRepositorySettings,
} from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

import styles from "@/components/repositories/repository-detail.module.css";

type RepositorySettingsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function RepositorySettingsPage({ params }: RepositorySettingsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  if (session.status !== "authenticated") {
    return (
      <RepositorySection description={dictionary.settingsPage.description} title={dictionary.settingsPage.title}>
        <AuthRequired
          dictionary={dictionary}
          returnTo={repositoryPath(locale, owner, repository, "settings")}
          session={session}
        />
      </RepositorySection>
    );
  }
  const repositoryResult = await getRepositorySettings(owner, repository);
  if (!repositoryResult.ok && repositoryResult.reason === "not-found") notFound();
  if (!repositoryResult.ok && repositoryResult.reason === "forbidden") {
    return (
      <EmptyState
        body={dictionary.settingsPage.notAuthorized}
        icon={<LockKeyhole aria-hidden="true" />}
        title={dictionary.errors.forbidden}
        tone="warning"
      />
    );
  }
  if (!repositoryResult.ok) {
    return (
      <EmptyState
        body={dictionary.home.apiUnavailableBody}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.repository.unavailable}
        tone="warning"
      />
    );
  }
  const data = repositoryResult.data;
  const [organizationResult, environmentsResult, categoriesResult] = await Promise.all([
    getOrganization(data.owner),
    getActionsEnvironments(data.owner, data.slug),
    getDiscussionCategories(data.owner, data.slug),
  ]);
  const canDelete = organizationResult.ok && organizationResult.data.role === "owner";
  return (
    <RepositorySection description={dictionary.settingsPage.description} title={dictionary.settingsPage.title}>
      {data.archivedAt ? (
        <EmptyState
          body={dictionary.repositoryLifecycle.archivedSettings}
          icon={<Archive aria-hidden="true" />}
          title={dictionary.repositoryLifecycle.banner}
          tone="warning"
        />
      ) : (
        <RepositoryPanel
          description={dictionary.settingsPage.generalDescription}
          title={dictionary.settingsPage.generalTitle}
        >
          <RepositorySettingsForm dictionary={dictionary} repository={data} session={session} />
        </RepositoryPanel>
      )}
      <RepositoryPanel title={dictionary.settingsPage.repositoryIdentity}>
        <dl className={styles.details}>
          <div>
            <dt>{dictionary.settingsPage.owner}</dt>
            <dd>{data.owner}</dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.slug}</dt>
            <dd>{data.slug}</dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.displayName}</dt>
            <dd>{data.displayName}</dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.descriptionLabel}</dt>
            <dd>{data.description || dictionary.common.noDescription}</dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.visibility}</dt>
            <dd>
              <LockKeyhole aria-hidden="true" size={14} /> {dictionary.common[data.visibility]}
            </dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.loreRepositoryId}</dt>
            <dd>
              <code>{data.loreRepositoryId}</code>
            </dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.loreUrl}</dt>
            <dd>
              <code>{data.loreUrl}</code>
            </dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.defaultBranch}</dt>
            <dd>
              <code>{data.defaultBranch}</code>
            </dd>
          </div>
        </dl>
      </RepositoryPanel>
      {!data.archivedAt && (
        <>
          <DiscussionCategoryPanel
            dictionary={dictionary}
            owner={data.owner}
            repository={data.slug}
            result={categoriesResult}
            session={session}
          />
          <RepositoryPanel
            description={dictionary.settingsPage.accessDescription}
            title={dictionary.settingsPage.accessTitle}
          >
            <RepositoryAccessSettings dictionary={dictionary} locale={locale} repository={data} session={session} />
          </RepositoryPanel>
          <RepositoryPanel
            description={dictionary.actionsEnvironments.description}
            title={dictionary.actionsEnvironments.title}
          >
            {environmentsResult.ok ? (
              <ActionsEnvironmentSettings
                dictionary={dictionary}
                environments={environmentsResult.data}
                owner={data.owner}
                repository={data.slug}
                session={session}
              />
            ) : (
              <p>{dictionary.actionsEnvironments.unavailable}</p>
            )}
          </RepositoryPanel>
          <RepositoryPanel
            description={dictionary.actionsSettings.repositoryDescription}
            title={dictionary.actionsSettings.title}
          >
            <ActionsContextSettings
              dictionary={dictionary}
              environmentNames={environmentsResult.ok ? environmentsResult.data.map((item) => item.name) : []}
              locale={locale}
              session={session}
              target={{ kind: "repository", owner: data.owner, repository: data.slug }}
            />
          </RepositoryPanel>
          <RepositoryPanel
            description={dictionary.webhookSettings.description}
            title={dictionary.webhookSettings.title}
          >
            <RepositoryWebhookSettings
              dictionary={dictionary}
              locale={locale}
              owner={data.owner}
              repository={data.slug}
              session={session}
            />
          </RepositoryPanel>
        </>
      )}
      <RepositoryPanel
        description={dictionary.repositoryLifecycle.settingsDescription}
        title={dictionary.repositoryLifecycle.settingsTitle}
      >
        <RepositoryLifecycleSettings dictionary={dictionary} repository={data} session={session} />
      </RepositoryPanel>
      {canDelete && (
        <RepositoryPanel
          description={dictionary.repositoryLifecycle.deleteSettingsDescription}
          title={dictionary.repositoryLifecycle.deleteTitle}
        >
          <RepositoryDeleteSettings dictionary={dictionary} locale={locale} repository={data} session={session} />
        </RepositoryPanel>
      )}
    </RepositorySection>
  );
}

function DiscussionCategoryPanel({
  dictionary,
  owner,
  repository,
  result,
  session,
}: {
  dictionary: Awaited<ReturnType<typeof getDictionary>>;
  owner: string;
  repository: string;
  result: APIResult<DiscussionCategoriesPage>;
  session: Extract<AuthSession, { status: "authenticated" }>;
}) {
  if (!result.ok || !result.data.viewerCanManage) return null;
  return (
    <RepositoryPanel
      description={dictionary.discussionsPage.categoriesDescription}
      title={dictionary.discussionsPage.categoriesTitle}
    >
      <DiscussionCategorySettings
        categories={result.data.categories}
        dictionary={dictionary}
        owner={owner}
        repository={repository}
        session={session}
      />
    </RepositoryPanel>
  );
}
