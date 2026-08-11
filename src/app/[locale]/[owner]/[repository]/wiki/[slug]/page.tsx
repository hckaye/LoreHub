import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { WikiDocument } from "@/components/wiki/wiki-document";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import type { WikiPage, WikiRevision } from "@/lib/api-types";
import { getAuthSession } from "@/lib/auth-api";
import { getWikiHistory, getWikiPage, getWikiRevision } from "@/lib/lorehub-api";

type WikiDocumentPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string; slug: string }>;
  searchParams: Promise<{ version?: string }>;
};

export const dynamic = "force-dynamic";

export default async function WikiDocumentPage({ params, searchParams }: WikiDocumentPageProps) {
  const [{ locale: value, owner, repository, slug }, query] = await Promise.all([params, searchParams]);
  const locale = isLocale(value) ? value : "en";
  const [dictionary, document, session] = await Promise.all([
    getDictionary(locale),
    loadWikiDocument(owner, repository, slug, parseVersion(query.version)),
    getAuthSession(),
  ]);
  const unavailable = (
    <RepositorySection description={dictionary.wikiPage.description} title={dictionary.wikiPage.title}>
      <EmptyState
        body={dictionary.wikiPage.unavailableBody}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.wikiPage.unavailableTitle}
        tone="warning"
      />
    </RepositorySection>
  );
  if (!document.ok && document.reason === "not-found") notFound();
  if (!document.ok) return unavailable;
  return (
    <RepositorySection description={dictionary.wikiPage.description} title={document.current.title}>
      <WikiDocument
        current={document.current}
        dictionary={dictionary}
        history={document.history}
        locale={locale}
        owner={owner}
        repository={repository}
        revision={document.revision}
        session={session}
      />
    </RepositorySection>
  );
}

type WikiDocumentResult =
  | { ok: true; current: WikiPage; history: WikiRevision[]; revision: WikiRevision | null }
  | { ok: false; reason: "not-found" | "unavailable" };

async function loadWikiDocument(
  owner: string,
  repository: string,
  slug: string,
  requestedVersion: number | null,
): Promise<WikiDocumentResult> {
  const current = await getWikiPage(owner, repository, slug);
  if (!current.ok) return wikiFailure(current.reason);
  const revisionRequest =
    requestedVersion !== null && requestedVersion !== current.data.version
      ? getWikiRevision(owner, repository, slug, requestedVersion)
      : Promise.resolve(null);
  const [history, revision] = await Promise.all([getWikiHistory(owner, repository, slug), revisionRequest]);
  if (!history.ok) return wikiFailure(history.reason);
  if (revision && !revision.ok) return wikiFailure(revision.reason);
  return { ok: true, current: current.data, history: history.data, revision: revision?.data ?? null };
}

function wikiFailure(reason: string): WikiDocumentResult {
  return { ok: false, reason: reason === "not-found" ? "not-found" : "unavailable" };
}

function parseVersion(value: string | undefined): number | null {
  if (!value || !/^\d+$/.test(value)) return null;
  const version = Number(value);
  return Number.isSafeInteger(version) && version > 0 ? version : null;
}
