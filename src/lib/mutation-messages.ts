import type { Dictionary } from "@/i18n";

import type { MutationFailureKind } from "./auth-client";

export function mutationFailureMessage(kind: MutationFailureKind, dictionary: Dictionary): string {
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
