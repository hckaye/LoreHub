import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { BlobView } from "@/components/repositories/blob-view";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getFileHistory, getLoreFile, getLoreTree, getPublicRepository } from "@/lib/lorehub-api";

type FilePageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ branch?: string; revision?: string; path?: string }>;
};

export const dynamic = "force-dynamic";

export default async function FilePage({ params, searchParams }: FilePageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const dictionary = await getDictionary(locale);
  const repository = await getPublicRepository(owner, slug);
  if (!repository.ok && repository.reason === "not-found") notFound();
  if (!repository.ok || !query.path) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.repository.unavailable}
        tone="warning"
      />
    );
  }
  const file = await getLoreFile(owner, slug, { branch: query.branch, revision: query.revision, path: query.path });
  if (!file.ok) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.repository.unavailable}
        tone="warning"
      />
    );
  }
  const parentPath = file.data.path.split("/").slice(0, -1).join("/");
  const [tree, history] = await Promise.all([
    getLoreTree(owner, slug, { revision: file.data.revision, path: parentPath }),
    file.data.kind === "file"
      ? getFileHistory(owner, slug, { revision: file.data.revision, path: file.data.path })
      : Promise.resolve(null),
  ]);
  const entries = tree.ok ? tree.data.entries : [];
  const historyEntries = history?.ok ? history.data.entries : [];
  return (
    <BlobView
      branch={query.branch}
      dictionary={dictionary}
      entries={entries}
      file={file.data}
      history={historyEntries}
      locale={locale}
      owner={owner}
      repository={slug}
      repositoryName={repository.data.slug}
    />
  );
}
