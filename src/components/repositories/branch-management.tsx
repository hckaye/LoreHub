"use client";

import { GitBranch, LockKeyhole, ShieldCheck, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Branch, BranchOverview, BranchRule, Repository } from "@/lib/api-types";
import { deleteJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { brandedAuthUrl, repositoryBranchesPath, repositoryPath } from "@/lib/routes";

import { StatusBadge } from "../ui/status-badge";
import { BranchCreateForm } from "./branch-create-form";
import styles from "./branch-management.module.css";
import { BranchRuleEditor } from "./branch-rule-editor";

type BranchManagementProps = {
  locale: Locale;
  repository: Repository;
  overview: BranchOverview;
  initialRules: BranchRule[];
  session: AuthSession;
  dictionary: Dictionary;
};

export function BranchManagement(props: BranchManagementProps) {
  const [branches, setBranches] = useState(props.overview.branches);
  const [rules, setRules] = useState(props.initialRules);
  const [message, setMessage] = useState("");
  const [messageIsError, setMessageIsError] = useState(false);
  const [archiving, setArchiving] = useState<string | null>(null);
  const router = useRouter();
  const copy = props.dictionary.branchManagement;
  const authenticated = props.session.status === "authenticated" ? props.session : null;
  const protectedBranches = useMemo(
    () => new Set(branches.filter((branch) => branchProtected(rules, branch.name)).map((branch) => branch.id)),
    [branches, rules],
  );

  function showMessage(value: string, error: boolean) {
    setMessage(value);
    setMessageIsError(error);
  }

  async function archive(branch: Branch) {
    if (!authenticated || !window.confirm(copy.archiveConfirm.replace("{branch}", branch.name))) return;
    setArchiving(branch.id);
    showMessage("", false);
    const base = `/api/v1/repositories/${encodeURIComponent(props.repository.owner)}/${encodeURIComponent(
      props.repository.slug,
    )}/branches/${branch.name.split("/").map(encodeURIComponent).join("/")}`;
    const result = await deleteJson<null>(base, authenticated.csrfToken);
    setArchiving(null);
    if (!result.ok) {
      showMessage(mutationFailureMessage(result.kind, props.dictionary), true);
      return;
    }
    setBranches((current) => current.filter((item) => item.id !== branch.id));
    showMessage(copy.archived, false);
    router.refresh();
  }

  return (
    <div className={styles.stack}>
      {message && (
        <p className={messageIsError ? styles.error : styles.success} role="status">
          {message}
        </p>
      )}
      <section className={styles.panel}>
        <div className={styles.heading}>
          <div>
            <h2>{copy.createTitle}</h2>
            <p>{copy.createDescription}</p>
          </div>
        </div>
        {authenticated && props.overview.viewerCanPush ? (
          <BranchCreateForm
            branches={branches}
            defaultBranch={props.repository.defaultBranch}
            dictionary={props.dictionary}
            onCreated={(branch) => {
              setBranches((current) => [...current.filter((item) => item.id !== branch.id), branch].sort(sortBranches));
              router.refresh();
            }}
            onMessage={showMessage}
            owner={props.repository.owner}
            repository={props.repository.slug}
            session={authenticated}
          />
        ) : (
          <BranchWriteNotice {...props} />
        )}
      </section>
      <section className={styles.panel}>
        <div className={styles.heading}>
          <div>
            <h2>{copy.listTitle}</h2>
            <p>{copy.listDescription}</p>
          </div>
          <strong>{branches.length}</strong>
        </div>
        {branches.length === 0 ? (
          <p className={styles.muted}>{copy.noBranches}</p>
        ) : (
          <div className={styles.branchList}>
            {branches.map((branch) => {
              const isDefault = branch.name === props.repository.defaultBranch;
              const isProtected = protectedBranches.has(branch.id);
              const codeURL = `${repositoryPath(
                props.locale,
                props.repository.owner,
                props.repository.slug,
              )}?branch=${encodeURIComponent(branch.name)}`;
              return (
                <article className={styles.branchRow} key={branch.id}>
                  <GitBranch aria-hidden="true" size={18} />
                  <div className={styles.branchDetails}>
                    <div className={styles.branchName}>
                      <Link href={codeURL}>{branch.name}</Link>
                      {isDefault && <StatusBadge tone="accent">{copy.defaultBranch}</StatusBadge>}
                      {isProtected && (
                        <StatusBadge>
                          <ShieldCheck aria-hidden="true" size={12} /> {copy.protectedBranch}
                        </StatusBadge>
                      )}
                    </div>
                    <p>
                      <code title={branch.latestRevision}>{shortRevision(branch.latestRevision)}</code>
                      {branch.category && <span>{branch.category}</span>}
                      {branch.creator && <span>{copy.createdBy.replace("{creator}", branch.creator)}</span>}
                    </p>
                  </div>
                  {authenticated && props.overview.viewerCanPush && !isDefault && !isProtected && (
                    <button
                      className={styles.dangerButton}
                      disabled={archiving !== null}
                      onClick={() => void archive(branch)}
                      type="button"
                    >
                      <Trash2 aria-hidden="true" size={14} />
                      {archiving === branch.id ? copy.archiving : copy.archive}
                    </button>
                  )}
                </article>
              );
            })}
          </div>
        )}
      </section>
      <section className={styles.panel}>
        <div className={styles.heading}>
          <div>
            <h2>{copy.protectionTitle}</h2>
            <p>{copy.protectionDescription}</p>
          </div>
          <ShieldCheck aria-hidden="true" size={20} />
        </div>
        <BranchRuleEditor
          canManage={props.overview.viewerCanManageRules}
          dictionary={props.dictionary}
          initialRules={rules}
          onMessage={showMessage}
          onRulesChanged={setRules}
          owner={props.repository.owner}
          repository={props.repository.slug}
          session={props.session}
        />
      </section>
    </div>
  );
}

function BranchWriteNotice(props: BranchManagementProps) {
  const copy = props.dictionary.branchManagement;
  if (props.session.status === "anonymous" || props.session.status === "expired") {
    const returnTo = repositoryBranchesPath(props.locale, props.repository.owner, props.repository.slug);
    return (
      <p className={styles.notice}>
        <LockKeyhole aria-hidden="true" size={16} />
        {copy.signInRequired}{" "}
        <Link href={brandedAuthUrl(props.locale, returnTo)}>{props.dictionary.common.signIn}</Link>
      </p>
    );
  }
  return (
    <p className={styles.notice}>
      <LockKeyhole aria-hidden="true" size={16} /> {copy.writeRequired}
    </p>
  );
}

function branchProtected(rules: BranchRule[], branch: string): boolean {
  return rules.some((rule) => rule.blockDirectPush && globMatches(rule.pattern, branch));
}

function globMatches(pattern: string, value: string): boolean {
  let expression = "^";
  for (const character of pattern) {
    if (character === "*") expression += ".*";
    else if (character === "?") expression += ".";
    else expression += character.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  }
  return new RegExp(`${expression}$`, "u").test(value);
}

function sortBranches(left: Branch, right: Branch): number {
  return left.name.localeCompare(right.name);
}

function shortRevision(revision: string): string {
  return revision.length > 12 ? revision.slice(0, 12) : revision;
}
