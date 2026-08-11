"use client";

import { Flag, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Milestone, MilestonePage } from "@/lib/api-types";
import { repositoryMilestonesPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import { MilestoneCard } from "./milestone-card";
import { MilestoneCreateForm } from "./milestone-create-form";
import styles from "./milestone-list.module.css";

type MilestoneListProps = {
  data: MilestonePage;
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
  session: AuthSession;
  state: "open" | "closed" | "all";
};

export function MilestoneList(props: MilestoneListProps) {
  const [items, setItems] = useState(props.data.milestones);
  const [showForm, setShowForm] = useState(false);
  const router = useRouter();
  const labels = props.dictionary.milestonesPage;
  const canWrite = props.data.viewerCanWrite && props.session.status === "authenticated";
  const basePath = repositoryMilestonesPath(props.locale, props.owner, props.repository);

  function add(created: Milestone) {
    setShowForm(false);
    if (props.state === "closed") {
      router.push(basePath);
      return;
    }
    setItems((current) => [created, ...current]);
  }

  function replace(updated: Milestone) {
    if (props.state !== "all" && updated.state !== props.state) {
      setItems((current) => current.filter((item) => item.number !== updated.number));
      return;
    }
    setItems((current) => current.map((item) => (item.number === updated.number ? updated : item)));
  }

  return (
    <div className={styles.page}>
      {canWrite && (
        <div className={styles.createArea}>
          <button className={styles.primaryButton} onClick={() => setShowForm((value) => !value)} type="button">
            <Plus aria-hidden="true" size={16} />
            {labels.newMilestone}
          </button>
          {showForm && (
            <MilestoneCreateForm
              dictionary={props.dictionary}
              onCancel={() => setShowForm(false)}
              onCreated={add}
              owner={props.owner}
              repository={props.repository}
              session={props.session}
            />
          )}
        </div>
      )}
      {items.length === 0 ? (
        <EmptyState body={labels.emptyBody} icon={<Flag aria-hidden="true" />} title={labels.emptyTitle} />
      ) : (
        <div className={styles.list}>
          {items.map((milestone) => (
            <MilestoneCard
              dictionary={props.dictionary}
              key={milestone.id}
              locale={props.locale}
              milestone={milestone}
              onChange={replace}
              onDelete={(number) => setItems((current) => current.filter((item) => item.number !== number))}
              owner={props.owner}
              repository={props.repository}
              session={props.session}
            />
          ))}
        </div>
      )}
      {(props.data.page > 1 || props.data.hasNext) && (
        <nav aria-label={labels.pagination} className={styles.pagination}>
          {props.data.page > 1 && (
            <Link href={pagePath(basePath, props.state, props.data.page - 1)}>{labels.previous}</Link>
          )}
          <span>{labels.page.replace("{page}", String(props.data.page))}</span>
          {props.data.hasNext && <Link href={pagePath(basePath, props.state, props.data.page + 1)}>{labels.next}</Link>}
        </nav>
      )}
    </div>
  );
}

function pagePath(basePath: string, state: string, page: number): string {
  const query = new URLSearchParams({ state, page: String(page) });
  return `${basePath}?${query.toString()}`;
}
