"use client";

import { FormEvent, useEffect, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Repository } from "@/lib/api-types";

import styles from "./repository-access-settings.module.css";

type RepositoryAccessSettingsProps = {
  repository: Repository;
  session: Extract<AuthSession, { status: "authenticated" }>;
  dictionary: Dictionary;
};

type Collaborator = {
  userId: string;
  username: string;
  displayName: string;
  role: string;
  active: boolean;
  source: string;
};

type Team = { id: string; slug: string; displayName: string };
type Link = { id: string; sourceRepository: string; targetRepository: string; kind: string };
type Policy = { allowCrossRepositoryLinks: boolean; obliterateEnabled: boolean };
type MutationResult<T> = { ok: boolean; data?: T };

const roles = ["admin", "maintain", "write", "triage", "read"];

export function RepositoryAccessSettings({ repository, session, dictionary }: RepositoryAccessSettingsProps) {
  const copy = dictionary.settingsPage;
  const [collaborators, setCollaborators] = useState<Collaborator[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [links, setLinks] = useState<Link[]>([]);
  const [policy, setPolicy] = useState<Policy>({ allowCrossRepositoryLinks: false, obliterateEnabled: false });
  const [username, setUsername] = useState("");
  const [role, setRole] = useState("read");
  const [team, setTeam] = useState("");
  const [teamRole, setTeamRole] = useState("read");
  const [grantUser, setGrantUser] = useState("");
  const [grantActive, setGrantActive] = useState(true);
  const [targetOwner, setTargetOwner] = useState("");
  const [targetRepository, setTargetRepository] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const base = `/api/v1/repositories/${encodeURIComponent(repository.owner)}/${encodeURIComponent(repository.slug)}`;

  useEffect(() => {
    let active = true;
    async function load() {
      try {
        const responses = await Promise.all([
          fetch(`${base}/collaborators`, { credentials: "include" }),
          fetch(`${base}/links`, { credentials: "include" }),
          fetch(`/api/v1/organizations/${encodeURIComponent(repository.owner)}/teams`, { credentials: "include" }),
          fetch(`${base}/policy`, { credentials: "include" }),
        ]);
        if (responses.some((response) => !response.ok)) {
          throw new Error("load_failed");
        }
        const [collaboratorResponse, linkResponse, teamResponse, policyResponse] = responses;
        const [collaboratorData, linkData, teamData, policyData] = await Promise.all([
          collaboratorResponse.json() as Promise<{ collaborators: Collaborator[] }>,
          linkResponse.json() as Promise<{ links: Link[] }>,
          teamResponse.json() as Promise<{ teams: Team[] }>,
          policyResponse.json() as Promise<Policy>,
        ]);
        if (!active) return;
        setCollaborators(collaboratorData.collaborators);
        setLinks(linkData.links);
        setTeams(teamData.teams);
        setTeam(teamData.teams[0]?.slug ?? "");
        setPolicy(policyData);
      } catch {
        if (active) setError(copy.loadFailed);
      } finally {
        if (active) setLoading(false);
      }
    }
    void load();
    return () => {
      active = false;
    };
  }, [base, copy.loadFailed, repository.owner]);

  async function mutate<T>(path: string, method: string, body?: unknown): Promise<MutationResult<T>> {
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const response = await fetch(path, {
        method,
        credentials: "include",
        headers: { Accept: "application/json", "Content-Type": "application/json", "X-CSRF-Token": session.csrfToken },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      if (!response.ok) throw new Error("save_failed");
      const data = response.status === 204 ? undefined : ((await response.json()) as T);
      setNotice(copy.saved);
      return { ok: true, data };
    } catch {
      setError(copy.saveFailed);
      return { ok: false };
    } finally {
      setSaving(false);
    }
  }

  async function saveCollaborator(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = username.trim();
    if (!trimmed) return;
    const result = await mutate<Collaborator>(`${base}/collaborators/${encodeURIComponent(trimmed)}`, "PUT", {
      role,
      active: true,
    });
    if (result.ok && result.data) {
      setCollaborators((current) => [
        ...current.filter((item) => item.username !== trimmed),
        result.data as Collaborator,
      ]);
      setUsername("");
    }
  }

  async function removeCollaborator(item: Collaborator) {
    if ((await mutate<Collaborator>(`${base}/collaborators/${encodeURIComponent(item.username)}`, "DELETE")).ok) {
      setCollaborators((current) =>
        current.map((value) => (value.username === item.username ? { ...value, active: false } : value)),
      );
    }
  }

  async function saveTeamAccess(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!team) return;
    const organizationPath = encodeURIComponent(repository.owner);
    const teamPath = encodeURIComponent(team);
    const repositoryPath = encodeURIComponent(repository.slug);
    await mutate<Team>(
      `/api/v1/organizations/${organizationPath}/teams/${teamPath}/repositories/${organizationPath}/${repositoryPath}`,
      "PUT",
      { role: teamRole },
    );
  }

  async function removeTeamAccess() {
    if (!team) return;
    const organizationPath = encodeURIComponent(repository.owner);
    const teamPath = encodeURIComponent(team);
    const repositoryPath = encodeURIComponent(repository.slug);
    await mutate(
      `/api/v1/organizations/${organizationPath}/teams/${teamPath}/repositories/${organizationPath}/${repositoryPath}`,
      "DELETE",
    );
  }

  async function savePolicy(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = await mutate<Policy>(`${base}/policy`, "PUT", policy);
    if (result.ok && result.data) setPolicy(result.data);
  }

  async function saveGrant(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!grantUser.trim()) return;
    if (
      (
        await mutate(`${base}/obliterate/${encodeURIComponent(grantUser.trim())}`, "PUT", {
          active: grantActive,
        })
      ).ok
    ) {
      setGrantUser("");
    }
  }

  async function declareLink(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!targetOwner.trim() || !targetRepository.trim()) return;
    const result = await mutate<{ link: Link }>(`${base}/links`, "POST", {
      targetOwner: targetOwner.trim(),
      targetRepository: targetRepository.trim(),
    });
    if (result.ok && result.data?.link) {
      setLinks((current) => [...current, result.data!.link]);
      setTargetOwner("");
      setTargetRepository("");
    }
  }

  if (loading) return <p className={styles.muted}>{dictionary.common.loading}</p>;
  if (error && collaborators.length === 0 && teams.length === 0) {
    return <p className={styles.error}>{error}</p>;
  }

  return (
    <div className={styles.stack}>
      {error && <p className={styles.error}>{error}</p>}
      {notice && <p className={styles.success}>{notice}</p>}
      <section>
        <h3>{copy.accessTitle}</h3>
        <p className={styles.muted}>{copy.accessDescription}</p>
        <form className={styles.form} onSubmit={saveCollaborator}>
          <div className={styles.fields}>
            <div className={styles.field}>
              <label htmlFor="collaborator-username">{copy.username}</label>
              <input
                id="collaborator-username"
                onChange={(event) => setUsername(event.target.value)}
                value={username}
              />
            </div>
            <div className={styles.field}>
              <label htmlFor="collaborator-role">{copy.role}</label>
              <select id="collaborator-role" onChange={(event) => setRole(event.target.value)} value={role}>
                {roles.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </div>
            <button className={styles.button} disabled={saving || !username.trim()} type="submit">
              {copy.addCollaborator}
            </button>
          </div>
        </form>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>{copy.username}</th>
              <th>{copy.role}</th>
              <th>{copy.source}</th>
              <th>{copy.active}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {collaborators.map((item) => (
              <tr key={`${item.source}-${item.username}`}>
                <td>
                  {item.displayName || item.username}
                  <br />
                  <small>{item.username}</small>
                </td>
                <td>
                  <span className={styles.tag}>{item.role}</span>
                </td>
                <td>{item.source}</td>
                <td>{item.active ? "✓" : "—"}</td>
                <td>
                  {item.source === "direct" && item.active && (
                    <button
                      className={styles.secondaryButton}
                      disabled={saving}
                      onClick={() => void removeCollaborator(item)}
                      type="button"
                    >
                      {copy.removeCollaborator}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section>
        <h3>{copy.teamAccessTitle}</h3>
        <p className={styles.muted}>{copy.teamAccessDescription}</p>
        <form className={styles.form} onSubmit={saveTeamAccess}>
          {teams.length === 0 ? (
            <p className={styles.muted}>{copy.noTeams}</p>
          ) : (
            <div className={styles.fields}>
              <div className={styles.field}>
                <label htmlFor="repository-team">{copy.teamAccessTitle}</label>
                <select id="repository-team" onChange={(event) => setTeam(event.target.value)} value={team}>
                  {teams.map((item) => (
                    <option key={item.id} value={item.slug}>
                      {item.displayName}
                    </option>
                  ))}
                </select>
              </div>
              <div className={styles.field}>
                <label htmlFor="repository-team-role">{copy.role}</label>
                <select
                  id="repository-team-role"
                  onChange={(event) => setTeamRole(event.target.value)}
                  value={teamRole}
                >
                  {roles.map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
                </select>
              </div>
              <div className={styles.actions}>
                <button className={styles.button} disabled={saving} type="submit">
                  {copy.saveTeamAccess}
                </button>
                <button
                  className={styles.secondaryButton}
                  disabled={saving}
                  onClick={() => void removeTeamAccess()}
                  type="button"
                >
                  {copy.removeTeamAccess}
                </button>
              </div>
            </div>
          )}
        </form>
      </section>

      <section>
        <h3>{copy.policyTitle}</h3>
        <p className={styles.muted}>{copy.policyDescription}</p>
        <form className={styles.form} onSubmit={savePolicy}>
          <div className={styles.checks}>
            <label className={styles.check}>
              <input
                checked={policy.allowCrossRepositoryLinks}
                onChange={(event) => setPolicy({ ...policy, allowCrossRepositoryLinks: event.target.checked })}
                type="checkbox"
              />
              {copy.allowLinks}
            </label>
            <label className={styles.check}>
              <input
                checked={policy.obliterateEnabled}
                onChange={(event) => setPolicy({ ...policy, obliterateEnabled: event.target.checked })}
                type="checkbox"
              />
              {copy.obliterateEnabled}
            </label>
          </div>
          <button className={styles.button} disabled={saving} type="submit">
            {copy.savePolicy}
          </button>
        </form>
      </section>

      <section>
        <h3>{copy.highRiskTitle}</h3>
        <p className={styles.muted}>{copy.highRiskDescription}</p>
        <form className={styles.form} onSubmit={saveGrant}>
          <div className={styles.fields}>
            <div className={styles.field}>
              <label htmlFor="obliterate-username">{copy.username}</label>
              <input
                id="obliterate-username"
                onChange={(event) => setGrantUser(event.target.value)}
                value={grantUser}
              />
            </div>
            <label className={styles.check}>
              <input checked={grantActive} onChange={(event) => setGrantActive(event.target.checked)} type="checkbox" />
              {copy.active}
            </label>
            <button className={styles.button} disabled={saving || !grantUser.trim()} type="submit">
              {grantActive ? copy.grantObliterate : copy.revokeObliterate}
            </button>
          </div>
        </form>
      </section>

      <section>
        <h3>{copy.linksTitle}</h3>
        <p className={styles.muted}>{copy.linksDescription}</p>
        <form className={styles.form} onSubmit={declareLink}>
          <div className={styles.fields}>
            <div className={styles.field}>
              <label htmlFor="link-owner">{copy.targetOwner}</label>
              <input id="link-owner" onChange={(event) => setTargetOwner(event.target.value)} value={targetOwner} />
            </div>
            <div className={styles.field}>
              <label htmlFor="link-repository">{copy.targetRepository}</label>
              <input
                id="link-repository"
                onChange={(event) => setTargetRepository(event.target.value)}
                value={targetRepository}
              />
            </div>
            <button
              className={styles.button}
              disabled={saving || !targetOwner.trim() || !targetRepository.trim()}
              type="submit"
            >
              {copy.declareLink}
            </button>
          </div>
        </form>
        {links.length === 0 ? (
          <p className={styles.muted}>{copy.noLinks}</p>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{copy.source}</th>
                <th>{copy.targetRepository}</th>
                <th>{copy.declaredOnly}</th>
              </tr>
            </thead>
            <tbody>
              {links.map((link) => (
                <tr key={`${link.id}-${link.targetRepository}`}>
                  <td>{link.sourceRepository}</td>
                  <td>{link.targetRepository}</td>
                  <td>
                    <span className={styles.tag}>{link.kind}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
