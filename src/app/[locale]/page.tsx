import { BookOpenText, ServerOff } from "lucide-react";
import Link from "next/link";

import { RepositoryGrid } from "@/components/repositories/repository-grid";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getPublicRepositories } from "@/lib/lorehub-api";

import styles from "./page.module.css";

type HomePageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function HomePage({ params }: HomePageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, repositories] = await Promise.all([getDictionary(locale), getPublicRepositories()]);

  return (
    <>
      <section className={styles.hero}>
        <div className={styles.heroInner}>
          <p className={styles.eyebrow}>{dictionary.home.eyebrow}</p>
          <h1>{dictionary.home.title}</h1>
          <p className={styles.intro}>{dictionary.home.intro}</p>
          <Link className={styles.architectureLink} href="https://github.com/EpicGames/lore" target="_blank">
            <BookOpenText aria-hidden="true" size={18} />
            {dictionary.home.architecture}
          </Link>
        </div>
      </section>

      <section className={styles.repositories} aria-labelledby="repositories-title">
        <div className={styles.sectionHeading}>
          <div>
            <h2 id="repositories-title">{dictionary.home.repositories}</h2>
            <p>{dictionary.home.repositoriesDescription}</p>
          </div>
        </div>

        {!repositories.ok ? (
          <EmptyState
            icon={<ServerOff aria-hidden="true" />}
            title={dictionary.home.apiUnavailableTitle}
            body={dictionary.home.apiUnavailableBody}
            tone="warning"
          />
        ) : (
          <RepositoryGrid
            repositories={repositories.data}
            locale={locale}
            dictionary={dictionary}
            emptyTitle={dictionary.home.emptyTitle}
            emptyBody={dictionary.home.emptyBody}
          />
        )}
      </section>
    </>
  );
}
