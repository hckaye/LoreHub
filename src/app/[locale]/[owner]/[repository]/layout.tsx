import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";
import type { ReactNode } from "react";

import { RepositoryHeader } from "@/components/repositories/repository-header";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getPublicRepository } from "@/lib/lorehub-api";

import styles from "./repository-layout.module.css";

type RepositoryLayoutProps = {
  children: ReactNode;
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function RepositoryLayout({ children, params }: RepositoryLayoutProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, repository] = await Promise.all([getDictionary(locale), getPublicRepository(owner, slug)]);
  if (!repository.ok && repository.reason === "not-found") {
    notFound();
  }
  if (!repository.ok) {
    return (
      <div className={styles.unavailable}>
        <EmptyState
          body={dictionary.home.apiUnavailableBody}
          icon={<ServerOff aria-hidden="true" />}
          title={dictionary.repository.unavailable}
          tone="warning"
        />
      </div>
    );
  }
  return (
    <>
      <RepositoryHeader dictionary={dictionary} locale={locale} repository={repository.data} />
      <div className={styles.shell}>{children}</div>
    </>
  );
}
