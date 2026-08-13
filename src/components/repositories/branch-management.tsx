"use client";

import { GitBranch, LockKeyhole, Search, ShieldCheck, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Branch, BranchOverview, BranchRule, Repository } from "@/lib/api-types";
import { deleteJson } from "@/lib/auth-client";
import { shortRevision } from "@/lib/format";
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
  const [deleting, setDeleting] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<Branch | null>(null);
  const [query, setQuery] = useState("");
  const router = useRouter();
  const copy = props.dictionary.branchManagement;
  const readOnly = Boolean(props.repository.archivedAt);
  const authenticated = props.session.status === "authenticated" && !readOnly ? props.session : null;
  const protectedBranches = useMemo(
    () => new Set(branches.filter((branch) => branchProtected(rules, branch.name)).map((branch) => branch.id)),
    [branches, rules],
  );
  const filteredBranches = useMemo(() => {
    const trimmed = query.trim().toLowerCase();
    const sorted = [...branches].sort(sortBranches);
    if (!trimmed) return sorted;
    return sorted.filter((branch) => branch.name.toLowerCase().includes(trimmed));
  }, [branches, query]);

  function showMessage(value: string, error: boolean) {
    setMessage(value);
    setMessageIsError(error);
  }

  async function confirmDelete(branch: Branch) {
    if (!authenticated) return;
    setDeleting(branch.id);
    showMessage("", false);
    const base = `/api/v1/repositories/${encodeURIComponent(props.repository.owner)}/${encodeURIComponent(
      props.repository.slug,
    )}/branches/${branch.name.split("/").map(encodeURIComponent).join("/")}`;
    const result = await deleteJson<null>(base, authenticated.csrfToken);
    setDeleting(null);
    setPendingDelete(null);
    if (!result.ok) {
      showMessage(mutationFailureMessage(result.kind, props.dictionary), true);
      return;
    }
    setBranches((current) => current.filter((item) => item.id !== branch.id));
    showMessage(copy.deleted, false);
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
        <div className={styles.toolbar}>
          <label className={styles.searchField}>
            <Search aria-hidden="true" size={14} />
            <input
              onChange={(event) => setQuery(event.target.value)}
              placeholder={copy.searchPlaceholder}
              type="search"
              value={query}
            />
          </label>
        </div>
        {branches.length === 0 ? (
          <p className={styles.muted}>{copy.noBranches}</p>
        ) : filteredBranches.length === 0 ? (
          <p className={styles.muted}>{copy.noMatches}</p>
        ) : (
          <div className={styles.branchTable} role="table">
            <div className={styles.tableHead} role="row">
              <span className={styles.columnBranch} role="columnheader">
                {copy.columnBranch}
              </span>
              <span className={styles.columnUpdated} role="columnheader">
                {copy.columnUpdated}
              </span>
              <span className={styles.columnRevision} role="columnheader">
                {copy.columnRevision}
              </span>
              <span className={styles.columnActions} role="columnheader">
                <span className="sr-only">{copy.columnActions}</span>
              </span>
            </div>
            {filteredBranches.map((branch) => {
              const isDefault = branch.name === props.repository.defaultBranch;
              const isProtected = protectedBranches.has(branch.id);
              const codeURL = `${repositoryPath(
                props.locale,
                props.repository.owner,
                props.repository.slug,
              )}?branch=${encodeURIComponent(branch.name)}`;
              const canDeleteRow = Boolean(
                authenticated && props.overview.viewerCanPush && !isDefault && !isProtected,
              );
              const isConfirming = pendingDelete?.id === branch.id;
              return (
                <div className={styles.branchRow} key={branch.id} role="row">
                  <div className={styles.columnBranch} role="cell">
                    <GitBranch aria-hidden="true" size={16} />
                    <div className={styles.branchDetails}>
                      <Link href={codeURL} className={styles.branchName}>
                        {branch.name}
                      </Link>
                      <div className={styles.badges}>
                        {isDefault && <StatusBadge tone="accent">{copy.defaultBranch}</StatusBadge>}
                        {isProtected && (
                          <StatusBadge>
                            <ShieldCheck aria-hidden="true" size={12} /> {copy.protectedBranch}
                          </StatusBadge>
                        )}
                        {branch.category && <span className={styles.category}>{branch.category}</span>}
                      </div>
                    </div>
                  </div>
                  <div className={styles.columnUpdated} role="cell">
                    {branch.creator && <span>{copy.createdBy.replace("{creator}", branch.creator)}</span>}
                  </div>
                  <div className={styles.columnRevision} role="cell">
                    <code title={branch.latestRevision}>{shortRevision(branch.latestRevision)}</code>
                  </div>
                  <div className={styles.columnActions} role="cell">
                    {canDeleteRow && !isConfirming && (
                      <button
                        className={styles.dangerButton}
                        disabled={deleting !== null}
                        onClick={() => setPendingDelete(branch)}
                        type="button"
                      >
                        <Trash2 aria-hidden="true" size={14} />
                        {copy.delete}
                      </button>
                    )}
                    {isConfirming && (
                      <div className={styles.inlineConfirm}>
                        <span>{copy.deleteConfirm.replace("{branch}", branch.name)}</span>
                        <button
                          className={styles.dangerButton}
                          disabled={deleting !== null}
                          onClick={() => void confirmDelete(branch)}
                          type="button"
                        >
                          {deleting === branch.id ? copy.deleting : copy.deleteConfirmAction}
                        </button>
                        <button
                          className={styles.secondaryButton}
                          disabled={deleting !== null}
                          onClick={() => setPendingDelete(null)}
                          type="button"
                        >
                          {props.dictionary.common.cancel}
                        </button>
                      </div>
                    )}
                  </div>
                </div>
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
          canManage={props.overview.viewerCanManageRules && !readOnly}
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
