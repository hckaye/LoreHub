import { Building2 } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { OrganizationView } from "@/lib/api-types";

import styles from "./search-page.module.css";

type SearchOrganizationResultsProps = {
  count: number;
  dictionary: Dictionary;
  locale: Locale;
  organizations: OrganizationView[];
};

export function SearchOrganizationResults(props: SearchOrganizationResultsProps) {
  return (
    <section className={styles.section}>
      <h2>
        <Building2 aria-hidden="true" size={18} />
        {props.dictionary.searchPage.organizations}
        <span>{props.count}</span>
      </h2>
      {props.organizations.length ? (
        <ul className={styles.list}>
          {props.organizations.map((organization) => (
            <li key={organization.id}>
              <Link href={`/${props.locale}/organizations/${encodeURIComponent(organization.slug)}`}>
                <strong>{organization.displayName}</strong>
                <span>
                  {organization.slug} · {organization.visibility}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <p className={styles.sectionEmpty}>{props.dictionary.searchPage.sectionEmpty}</p>
      )}
    </section>
  );
}
