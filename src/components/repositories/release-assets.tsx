"use client";

import { Download, Plus, Trash2 } from "lucide-react";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Release } from "@/lib/api-types";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { addReleaseAsset, deleteReleaseAsset } from "@/lib/release-client";

import styles from "./release-list.module.css";

type ReleaseAssetsProps = {
  dictionary: Dictionary;
  owner: string;
  repository: string;
  release: Release;
  session: AuthSession;
  onChange(release: Release): void;
  onFailure(message: string): void;
};

export function ReleaseAssets(props: ReleaseAssetsProps) {
  const [showForm, setShowForm] = useState(false);
  const [name, setName] = useState("");
  const [externalUrl, setExternalURL] = useState("");
  const [saving, setSaving] = useState(false);
  const labels = props.dictionary.releasesPage;

  async function add(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (props.session.status !== "authenticated") return;
    setSaving(true);
    const result = await addReleaseAsset(
      props.owner,
      props.repository,
      props.release.id,
      { name: name.trim(), externalUrl: externalUrl.trim(), expectedVersion: props.release.version },
      props.session.csrfToken,
    );
    setSaving(false);
    if (!result.ok) {
      props.onFailure(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setName("");
    setExternalURL("");
    setShowForm(false);
    props.onChange(result.data);
  }

  async function remove(assetID: string) {
    if (props.session.status !== "authenticated" || !window.confirm(labels.confirmDeleteAsset)) return;
    setSaving(true);
    const result = await deleteReleaseAsset(
      props.owner,
      props.repository,
      props.release.id,
      assetID,
      props.release.version,
      props.session.csrfToken,
    );
    setSaving(false);
    if (!result.ok) {
      props.onFailure(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    props.onChange(result.data);
  }

  return (
    <section className={styles.assets}>
      <div className={styles.assetsHeading}>
        <h3>{labels.assets}</h3>
        {props.release.viewerCanWrite && props.session.status === "authenticated" && (
          <button className={styles.textButton} onClick={() => setShowForm((visible) => !visible)} type="button">
            <Plus aria-hidden="true" size={15} />
            {labels.addAsset}
          </button>
        )}
      </div>
      {props.release.assets.length === 0 ? (
        <p className={styles.emptyAssets}>{labels.noAssets}</p>
      ) : (
        <ul className={styles.assetList}>
          {props.release.assets.map((asset) => (
            <li key={asset.id}>
              <a href={asset.externalUrl} rel="noopener noreferrer" target="_blank">
                <Download aria-hidden="true" size={16} />
                {asset.name}
              </a>
              {props.release.viewerCanWrite && props.session.status === "authenticated" && (
                <button
                  aria-label={labels.deleteAsset.replace("{name}", asset.name)}
                  disabled={saving}
                  onClick={() => remove(asset.id)}
                  type="button"
                >
                  <Trash2 aria-hidden="true" size={15} />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {showForm && (
        <form className={styles.assetForm} onSubmit={add}>
          <input
            aria-label={labels.assetName}
            maxLength={255}
            onChange={(event) => setName(event.target.value)}
            placeholder={labels.assetName}
            required
            value={name}
          />
          <input
            aria-label={labels.assetURL}
            maxLength={8192}
            onChange={(event) => setExternalURL(event.target.value)}
            placeholder="https://downloads.example.com/package.zip"
            required
            type="url"
            value={externalUrl}
          />
          <button className={styles.secondaryButton} disabled={saving} type="submit">
            {saving ? labels.saving : labels.addAsset}
          </button>
        </form>
      )}
    </section>
  );
}
