"use client";

import { FormEvent, useEffect, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession } from "@/lib/api-types";

import styles from "./organization-team-settings.module.css";

type Props = {
  organization: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
  dictionary: Dictionary;
};

type Team = {
  id: string;
  slug: string;
  displayName: string;
  description: string;
};

type Member = {
  userId: string;
  username: string;
  displayName: string;
  role: string;
  active: boolean;
};

type OrganizationMember = {
  userId: string;
  username: string;
  displayName: string;
  role: string;
  active: boolean;
};

type MutationResult<T> = { ok: boolean; data?: T };

export function OrganizationTeamSettings({ organization, session, dictionary }: Props) {
  const copy = dictionary.organizationSettingsPage;
  const base = `/api/v1/organizations/${encodeURIComponent(organization)}`;
  const [teams, setTeams] = useState<Team[]>([]);
  const [selected, setSelected] = useState("");
  const [members, setMembers] = useState<Member[]>([]);
  const [organizationMembers, setOrganizationMembers] = useState<OrganizationMember[]>([]);
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [memberUsername, setMemberUsername] = useState("");
  const [memberRole, setMemberRole] = useState("member");
  const [memberActive, setMemberActive] = useState(true);
  const [organizationUsername, setOrganizationUsername] = useState("");
  const [organizationRole, setOrganizationRole] = useState("member");
  const [organizationActive, setOrganizationActive] = useState(true);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    let mounted = true;
    async function load() {
      try {
        const [teamResponse, memberResponse] = await Promise.all([
          fetch(`${base}/teams`, { credentials: "include" }),
          fetch(`${base}/members`, { credentials: "include" }),
        ]);
        if (!teamResponse.ok || !memberResponse.ok) throw new Error("load_failed");
        const [data, memberData] = await Promise.all([
          teamResponse.json() as Promise<{ teams: Team[] }>,
          memberResponse.json() as Promise<{ members: OrganizationMember[] }>,
        ]);
        if (!mounted) return;
        setTeams(data.teams);
        setOrganizationMembers(memberData.members);
        const first = data.teams[0];
        setSelected(first?.slug ?? "");
        setName(first?.displayName ?? "");
        setDescription(first?.description ?? "");
      } catch {
        if (mounted) setError(copy.loadFailed);
      } finally {
        if (mounted) setLoading(false);
      }
    }
    void load();
    return () => {
      mounted = false;
    };
  }, [base, copy.loadFailed]);

  useEffect(() => {
    const current = teams.find((team) => team.slug === selected);
    if (!current) {
      return;
    }
    const teamSlug = current.slug;
    let mounted = true;
    async function loadMembers() {
      try {
        const response = await fetch(`${base}/teams/${encodeURIComponent(teamSlug)}/members`, {
          credentials: "include",
        });
        if (!response.ok) throw new Error("load_failed");
        const data = (await response.json()) as { members: Member[] };
        if (mounted) setMembers(data.members);
      } catch {
        if (mounted) setError(copy.loadFailed);
      }
    }
    void loadMembers();
    return () => {
      mounted = false;
    };
  }, [base, copy.loadFailed, selected, teams]);

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

  async function createTeam(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!slug.trim() || !name.trim()) return;
    const result = await mutate<Team>(`${base}/teams`, "POST", {
      slug: slug.trim(),
      displayName: name.trim(),
      description,
    });
    if (result.ok && result.data) {
      setTeams((current) => [...current, result.data as Team]);
      setSelected(result.data.slug);
      setSlug("");
    }
  }

  async function updateTeam(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected || !name.trim()) return;
    const result = await mutate<Team>(`${base}/teams/${encodeURIComponent(selected)}`, "PATCH", {
      displayName: name.trim(),
      description,
    });
    if (result.ok && result.data) {
      setTeams((current) => current.map((team) => (team.slug === selected ? (result.data as Team) : team)));
    }
  }

  async function deleteTeam() {
    if (!selected || !(await mutate(`${base}/teams/${encodeURIComponent(selected)}`, "DELETE")).ok) return;
    const remaining = teams.filter((team) => team.slug !== selected);
    setTeams(remaining);
    const next = remaining[0];
    setSelected(next?.slug ?? "");
    setName(next?.displayName ?? "");
    setDescription(next?.description ?? "");
  }

  async function saveMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected || !memberUsername.trim()) return;
    const result = await mutate<Member>(
      `${base}/teams/${encodeURIComponent(selected)}/members/${encodeURIComponent(memberUsername.trim())}`,
      "PUT",
      {
        role: memberRole,
        active: memberActive,
      },
    );
    if (result.ok && result.data) {
      setMembers((current) =>
        current.filter((member) => member.username !== memberUsername.trim()).concat(result.data as Member),
      );
      setMemberUsername("");
    }
  }

  async function saveOrganizationMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = organizationUsername.trim();
    if (!trimmed) return;
    const result = await mutate<OrganizationMember>(`${base}/members/${encodeURIComponent(trimmed)}`, "PUT", {
      role: organizationRole,
      active: organizationActive,
    });
    if (result.ok && result.data) {
      setOrganizationMembers((current) => [
        ...current.filter((member) => member.username !== trimmed),
        result.data as OrganizationMember,
      ]);
      setOrganizationUsername("");
    }
  }

  if (loading) return <p className={styles.muted}>{dictionary.common.loading}</p>;
  if (error && teams.length === 0) return <p className={styles.error}>{error}</p>;

  return (
    <div className={styles.stack}>
      {error && <p className={styles.error}>{error}</p>}
      {notice && <p className={styles.success}>{notice}</p>}
      <section>
        <h2>{copy.organizationMembersTitle}</h2>
        <p className={styles.muted}>{copy.organizationMembersDescription}</p>
        <form className={styles.form} onSubmit={saveOrganizationMember}>
          <div className={styles.fields}>
            <div className={styles.field}>
              <label htmlFor="organization-member-username">{copy.memberUsername}</label>
              <input
                id="organization-member-username"
                onChange={(event) => setOrganizationUsername(event.target.value)}
                value={organizationUsername}
              />
            </div>
            <div className={styles.field}>
              <label htmlFor="organization-member-role">{copy.organizationMemberRole}</label>
              <select
                id="organization-member-role"
                onChange={(event) => setOrganizationRole(event.target.value)}
                value={organizationRole}
              >
                <option value="member">member</option>
                <option value="maintainer">maintainer</option>
                <option value="owner">owner</option>
              </select>
            </div>
          </div>
          <div className={styles.actions}>
            <label>
              <input
                checked={organizationActive}
                onChange={(event) => setOrganizationActive(event.target.checked)}
                type="checkbox"
              />{" "}
              {copy.memberActive}
            </label>
            <button className={styles.button} disabled={saving || !organizationUsername.trim()} type="submit">
              {copy.saveOrganizationMember}
            </button>
          </div>
        </form>
        <table className={styles.members}>
          <thead>
            <tr>
              <th>{copy.memberUsername}</th>
              <th>{copy.organizationMemberRole}</th>
              <th>{copy.memberActive}</th>
            </tr>
          </thead>
          <tbody>
            {organizationMembers.map((member) => (
              <tr key={member.userId}>
                <td>
                  {member.displayName}
                  <br />
                  <small>{member.username}</small>
                </td>
                <td>{member.role}</td>
                <td>{member.active ? "✓" : "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      <section>
        <h2>{copy.teamTitle}</h2>
        <p className={styles.muted}>{copy.teamDescription}</p>
        <form className={styles.form} onSubmit={createTeam}>
          <div className={styles.fields}>
            <div className={styles.field}>
              <label htmlFor="new-team-slug">{copy.teamSlug}</label>
              <input id="new-team-slug" onChange={(event) => setSlug(event.target.value)} value={slug} />
            </div>
            <div className={styles.field}>
              <label htmlFor="team-name">{copy.teamName}</label>
              <input id="team-name" onChange={(event) => setName(event.target.value)} value={name} />
            </div>
            <div className={styles.field}>
              <label htmlFor="team-description">{copy.teamDescriptionLabel}</label>
              <textarea
                id="team-description"
                onChange={(event) => setDescription(event.target.value)}
                value={description}
              />
            </div>
          </div>
          <div className={styles.actions}>
            <button className={styles.button} disabled={saving || !slug.trim() || !name.trim()} type="submit">
              {copy.createTeam}
            </button>
          </div>
        </form>
      </section>
      {teams.length === 0 ? (
        <p className={styles.muted}>{copy.noTeams}</p>
      ) : (
        <>
          <section>
            <div className={styles.field}>
              <label htmlFor="selected-team">{copy.selectTeam}</label>
              <select
                id="selected-team"
                onChange={(event) => {
                  const next = teams.find((team) => team.slug === event.target.value);
                  if (!next) return;
                  setSelected(next.slug);
                  setName(next.displayName);
                  setDescription(next.description);
                  setMembers([]);
                }}
                value={selected}
              >
                {teams.map((team) => (
                  <option key={team.id} value={team.slug}>
                    {team.displayName}
                  </option>
                ))}
              </select>
            </div>
            <form className={styles.form} onSubmit={updateTeam}>
              <div className={styles.fields}>
                <div className={styles.field}>
                  <label htmlFor="selected-team-name">{copy.teamName}</label>
                  <input id="selected-team-name" onChange={(event) => setName(event.target.value)} value={name} />
                </div>
                <div className={styles.field}>
                  <label htmlFor="selected-team-description">{copy.teamDescriptionLabel}</label>
                  <textarea
                    id="selected-team-description"
                    onChange={(event) => setDescription(event.target.value)}
                    value={description}
                  />
                </div>
              </div>
              <div className={styles.actions}>
                <button className={styles.button} disabled={saving || !name.trim()} type="submit">
                  {copy.updateTeam}
                </button>
                <button
                  className={styles.dangerButton}
                  disabled={saving}
                  onClick={() => void deleteTeam()}
                  type="button"
                >
                  {copy.deleteTeam}
                </button>
              </div>
            </form>
          </section>
          <section>
            <h2>{copy.membersTitle}</h2>
            <p className={styles.muted}>{copy.memberDescription}</p>
            <form className={styles.form} onSubmit={saveMember}>
              <div className={styles.fields}>
                <div className={styles.field}>
                  <label htmlFor="member-username">{copy.memberUsername}</label>
                  <input
                    id="member-username"
                    onChange={(event) => setMemberUsername(event.target.value)}
                    value={memberUsername}
                  />
                </div>
                <div className={styles.field}>
                  <label htmlFor="member-role">{copy.memberRole}</label>
                  <select id="member-role" onChange={(event) => setMemberRole(event.target.value)} value={memberRole}>
                    <option value="member">member</option>
                    <option value="maintainer">maintainer</option>
                  </select>
                </div>
              </div>
              <div className={styles.actions}>
                <label>
                  <input
                    checked={memberActive}
                    onChange={(event) => setMemberActive(event.target.checked)}
                    type="checkbox"
                  />{" "}
                  {copy.memberActive}
                </label>
                <button className={styles.button} disabled={saving || !memberUsername.trim()} type="submit">
                  {copy.addMember}
                </button>
              </div>
            </form>
            <table className={styles.members}>
              <thead>
                <tr>
                  <th>{copy.memberUsername}</th>
                  <th>{copy.memberRole}</th>
                  <th>{copy.memberActive}</th>
                </tr>
              </thead>
              <tbody>
                {members.map((member) => (
                  <tr key={member.userId || member.username}>
                    <td>
                      {member.displayName}
                      <br />
                      <small>{member.username}</small>
                    </td>
                    <td>{member.role}</td>
                    <td>{member.active ? "✓" : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
        </>
      )}
    </div>
  );
}
