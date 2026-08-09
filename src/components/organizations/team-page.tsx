"use client";

import { UsersRound } from "lucide-react";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Team, TeamMember } from "@/lib/api-types";
import { deleteJson, patchJson, postJson } from "@/lib/auth-client";

import { EmptyState } from "../ui/empty-state";
import styles from "./team-page.module.css";

type TeamPageProps = {
  dictionary: Dictionary;
  members: TeamMember[];
  organizationSlug: string;
  session: AuthSession;
  team: Team | null;
};

export function TeamPage(props: TeamPageProps) {
  if (!props.team) {
    return (
      <div className={styles.page}>
        <EmptyState
          body={props.dictionary.home.apiUnavailableBody}
          icon={<UsersRound aria-hidden="true" />}
          title={props.dictionary.home.apiUnavailableTitle}
          tone="warning"
        />
      </div>
    );
  }
  return <TeamContent {...props} team={props.team} />;
}

type TeamContentProps = Omit<TeamPageProps, "team"> & { team: Team };

function TeamContent({ dictionary, members: initialMembers, organizationSlug, session, team }: TeamContentProps) {
  const teamData = team;
  const [members, setMembers] = useState(initialMembers);
  const [displayName, setDisplayName] = useState(teamData.displayName);
  const [description, setDescription] = useState(teamData.description);
  const [username, setUsername] = useState("");
  const [role, setRole] = useState<"member" | "maintainer">("member");
  const [status, setStatus] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const canManage =
    session.status === "authenticated" && teamData.viewerRole !== "member" && teamData.viewerRole !== "";

  async function saveSettings(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (session.status !== "authenticated") return;
    setPending(true);
    const result = await patchJson<Team>(
      teamRoute(organizationSlug, teamData.slug, "/settings"),
      {
        displayName,
        description,
      },
      session.csrfToken,
    );
    setPending(false);
    setStatus(result.ok ? dictionary.teamPage.settingsSaved : dictionary.forms.submitFailed);
  }

  async function addMember(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (session.status !== "authenticated" || !username.trim()) return;
    setPending(true);
    const result = await postJson<TeamMember>(
      teamRoute(organizationSlug, teamData.slug, "/members"),
      {
        username: username.trim(),
        role,
      },
      session.csrfToken,
    );
    setPending(false);
    if (result.ok) {
      setMembers((current) => [...current.filter((member) => member.userId !== result.data.userId), result.data]);
      setUsername("");
      setStatus(null);
    } else {
      setStatus(dictionary.forms.submitFailed);
    }
  }

  async function removeMember(member: TeamMember) {
    if (session.status !== "authenticated") return;
    setPending(true);
    const memberPath = `${teamRoute(organizationSlug, teamData.slug, "/members")}/${encodeURIComponent(
      member.username,
    )}`;
    const result = await deleteJson<{ removed: boolean }>(memberPath, session.csrfToken);
    setPending(false);
    if (result.ok) {
      setMembers((current) => current.filter((item) => item.userId !== member.userId));
    } else {
      setStatus(dictionary.forms.submitFailed);
    }
  }

  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <div className={styles.mark} aria-hidden="true">
          <UsersRound size={28} />
        </div>
        <div>
          <p className={styles.eyebrow}>{dictionary.teamPage.title}</p>
          <h1>{teamData.displayName}</h1>
          <p className={styles.slug}>
            {organizationSlug} / @{teamData.slug}
          </p>
          {teamData.description && <p className={styles.description}>{teamData.description}</p>}
        </div>
      </section>
      <section className={styles.panel}>
        <div className={styles.panelHeading}>
          <div>
            <h2>{dictionary.teamPage.members}</h2>
            <p>{teamData.memberCount}</p>
          </div>
        </div>
        {members.length > 0 ? (
          <ul className={styles.members}>
            {members.map((member) => (
              <li key={member.userId}>
                <div>
                  <strong>{member.displayName}</strong>
                  <span>
                    @{member.username} · {member.role}
                  </span>
                </div>
                {canManage && (
                  <button disabled={pending} onClick={() => removeMember(member)} type="button">
                    {dictionary.teamPage.remove}
                  </button>
                )}
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState
            body={dictionary.teamPage.noMembersBody}
            icon={<UsersRound aria-hidden="true" />}
            title={dictionary.teamPage.noMembers}
          />
        )}
      </section>
      {canManage && session.status === "authenticated" && (
        <>
          <form className={styles.form} onSubmit={saveSettings}>
            <h2>{dictionary.teamPage.settings}</h2>
            <label>
              <span>{dictionary.profile.displayName}</span>
              <input onChange={(event) => setDisplayName(event.target.value)} required value={displayName} />
            </label>
            <label>
              <span>{dictionary.forms.description}</span>
              <textarea onChange={(event) => setDescription(event.target.value)} rows={3} value={description} />
            </label>
            <div className={styles.actions}>
              <button disabled={pending} type="submit">
                {pending ? dictionary.common.loading : dictionary.teamPage.saveSettings}
              </button>
              {status && <span role="status">{status}</span>}
            </div>
          </form>
          <form className={styles.form} onSubmit={addMember}>
            <h2>{dictionary.teamPage.addMember}</h2>
            <div className={styles.addGrid}>
              <label>
                <span>{dictionary.teamPage.username}</span>
                <input onChange={(event) => setUsername(event.target.value)} required value={username} />
              </label>
              <label>
                <span>{dictionary.teamPage.role}</span>
                <select onChange={(event) => setRole(event.target.value as typeof role)} value={role}>
                  <option value="member">{dictionary.teamPage.member}</option>
                  <option value="maintainer">{dictionary.teamPage.maintainer}</option>
                </select>
              </label>
            </div>
            <div className={styles.actions}>
              <button disabled={pending} type="submit">
                {pending ? dictionary.common.loading : dictionary.teamPage.add}
              </button>
            </div>
          </form>
        </>
      )}
    </div>
  );
}

function teamRoute(organization: string, team: string, suffix: string): string {
  return `/api/v1/organizations/${encodeURIComponent(organization)}/teams/${encodeURIComponent(team)}${suffix}`;
}
