import { ServerOff } from "lucide-react";
import { notFound, redirect } from "next/navigation";

import { CommitHeader } from "@/components/repositories/commit-detail";
import { CommitStatusList } from "@/components/repositories/commit-status-list";
import { DiffView } from "@/components/repositories/diff-view";
import { RevisionComments } from "@/components/repositories/revision-comments";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import {
  getLoreDiff,
  getPublicRepository,
  getRevision,
  getRevisionComments,
  getRevisionStatuses,
} from "@/lib/lorehub-api";
import {
  lastRevisionCommentPage,
  parseRevisionCommentPageNumber,
  revisionCommentPageHref,
} from "@/lib/revision-comments";

import styles from "@/components/repositories/commit-detail.module.css";

type RevisionPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ commentPage?: string | string[]; revision?: string }>;
};

export const dynamic = "force-dynamic";

export default async function RevisionPage({ params, searchParams }: RevisionPageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const [dictionary, repository, session] = await Promise.all([
    getDictionary(locale),
    getPublicRepository(owner, slug),
    getAuthSession(),
  ]);
  if (!repository.ok && repository.reason === "not-found") notFound();
  if (!repository.ok || !query.revision) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const revision = await getRevision(owner, slug, query.revision);
  if (!revision.ok) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const commentPage = parseRevisionCommentPageNumber(query.commentPage);
  const [diff, statuses, comments] = await Promise.all([
    loadParentDiff(owner, slug, revision.data.revision, revision.data.parents[0]),
    getRevisionStatuses(owner, slug, revision.data.revision),
    getRevisionComments(owner, slug, revision.data.revision, commentPage),
  ]);
  const basePath = `/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(slug)}/commit`;
  if (comments.ok) {
    const lastPage = lastRevisionCommentPage(comments.data.totalCount, comments.data.perPage);
    if (commentPage > lastPage) redirect(revisionCommentPageHref(basePath, revision.data.revision, lastPage));
  }
  return (
    <div className={styles.detail}>
      <CommitHeader dictionary={dictionary} locale={locale} owner={owner} repository={slug} revision={revision.data} />
      <CommitStatusList dictionary={dictionary} locale={locale} {...statusListProps(statuses)} />
      {diff?.ok && <DiffView diff={diff.data} dictionary={dictionary} />}
      <RevisionComments
        basePath={basePath}
        comments={comments.ok ? comments.data : null}
        dictionary={dictionary}
        locale={locale}
        owner={owner}
        readOnly={Boolean(repository.data.archivedAt)}
        repository={slug}
        revision={revision.data.revision}
        session={session}
        unavailableReason={commentUnavailableReason(comments)}
      />
    </div>
  );
}

function commentUnavailableReason(result: Awaited<ReturnType<typeof getRevisionComments>>) {
  if (result.ok) return undefined;
  return result.reason === "forbidden" || result.reason === "unauthorized"
    ? ("forbidden" as const)
    : ("unavailable" as const);
}

function loadParentDiff(owner: string, repository: string, revision: string, parent?: string) {
  return parent ? getLoreDiff(owner, repository, parent, revision) : Promise.resolve(null);
}

function statusListProps(result: Awaited<ReturnType<typeof getRevisionStatuses>>) {
  if (result.ok) {
    return { state: result.data.state, statuses: result.data.statuses };
  }
  const forbidden = result.reason === "forbidden" || result.reason === "unauthorized";
  return { statuses: null, unavailableReason: forbidden ? ("forbidden" as const) : ("unavailable" as const) };
}
