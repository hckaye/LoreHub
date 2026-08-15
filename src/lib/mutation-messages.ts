import type { Dictionary } from "@/i18n";

import type { MutationFailureKind } from "./auth-client";

/** Reasons the API reports on conflict responses that have a dedicated UI message. */
const problemCodes: Record<string, keyof Dictionary["errors"]> = {
  hosted_lore_server_disabled: "hostedLoreServerDisabled",
  hosted_lore_server_entitlement_required: "noLoreServer",
  no_lore_server_available: "noLoreServer",
  default_server_unavailable: "loreServerUnavailable",
  explicit_server_unavailable: "loreServerUnavailable",
  organization_limit: "organizationLimit",
  repository_limit: "repositoryLimit",
};

export function mutationFailureMessage(
  kind: MutationFailureKind,
  dictionary: Dictionary,
  code: string | null = null,
): string {
  const problemMessage = code ? problemCodes[code] : undefined;
  if (problemMessage) {
    return dictionary.errors[problemMessage];
  }
  if (kind === "unauthorized") {
    return dictionary.errors.unauthorized;
  }
  if (kind === "forbidden") {
    return dictionary.errors.forbidden;
  }
  if (kind === "conflict") {
    return dictionary.errors.conflict;
  }
  if (kind === "invalid") {
    return dictionary.errors.invalid;
  }
  return dictionary.errors.unavailable;
}
