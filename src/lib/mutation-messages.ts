import type { Dictionary } from "@/i18n";

import type { MutationFailureKind } from "./auth-client";

/** Reasons the API reports when it cannot pick a Lore Server for a repository. */
const loreServerCodes: Record<string, keyof Dictionary["errors"]> = {
  hosted_lore_server_entitlement_required: "noLoreServer",
  no_lore_server_available: "noLoreServer",
  default_server_unavailable: "loreServerUnavailable",
  explicit_server_unavailable: "loreServerUnavailable",
};

export function mutationFailureMessage(
  kind: MutationFailureKind,
  dictionary: Dictionary,
  code: string | null = null,
): string {
  const loreServerMessage = code ? loreServerCodes[code] : undefined;
  if (loreServerMessage) {
    return dictionary.errors[loreServerMessage];
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
