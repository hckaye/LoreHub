import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { FileHistory } from "@/components/repositories/file-history";
import { RepositoryPanel } from "@/components/repositories/repository-section";
import { SafeReadme } from "@/components/repositories/safe-readme";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getFileHistory, getLoreFile, getPublicRepository } from "@/lib/lorehub-api";

import styles from "@/components/repositories/code-detail.module.css";

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
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const file = await getLoreFile(owner, slug, { branch: query.branch, revision: query.revision, path: query.path });
  if (!file.ok) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const rawQuery = new URLSearchParams({ revision: file.data.revision, path: file.data.path }).toString();
  const rawPath = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(slug)}/raw?${rawQuery}`;
  const readme = /(^|\/)readme(?:\.md)?$/i.test(file.data.path);
  const history =
    file.data.kind === "file"
      ? await getFileHistory(owner, slug, { revision: file.data.revision, path: file.data.path })
      : null;
  return (
    <RepositoryPanel description={file.data.path} title={dictionary.codeBrowser.fileTitle}>
      <div className={styles.panel}>
        <div className={styles.heading}>
          <div>
            <h2>{file.data.path}</h2>
            <p className={styles.meta}>
              {dictionary.codeBrowser.revision}: <code>{file.data.revision}</code> · {file.data.size}{" "}
              {dictionary.codeBrowser.bytes}
            </p>
          </div>
          <div className={styles.actions}>
            <a href={rawPath}>{dictionary.codeBrowser.raw}</a>
          </div>
        </div>
        {file.data.truncated ? (
          <div className={styles.status} data-tone="warning">
            {dictionary.codeBrowser.tooLarge}
          </div>
        ) : file.data.binary ? (
          <div className={styles.status}>{dictionary.codeBrowser.binary}</div>
        ) : readme ? (
          <SafeReadme content={file.data.content ?? ""} label={dictionary.codeBrowser.fileTitle} />
        ) : (
          <div className={styles.source}>
            <pre>{file.data.content ?? ""}</pre>
          </div>
        )}
        {history?.ok && (
          <section aria-labelledby="file-history-title">
            <h3 id="file-history-title">{dictionary.codeBrowser.fileHistory}</h3>
            <FileHistory
              dictionary={dictionary}
              entries={history.data.entries}
              locale={locale}
              owner={owner}
              repository={slug}
            />
          </section>
        )}
      </div>
    </RepositoryPanel>
  );
}
