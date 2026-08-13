"use client";

import { deleteJson, postJson, type MutationResult } from "./auth-client";
import {
  normalizeRunnerRegistration,
  runnerPath,
  runnerRegistrationTokenPath,
  type RunnerRegistration,
  type RunnerTarget,
} from "./runners";

export async function createRunnerRegistrationToken(
  target: RunnerTarget,
  csrfToken: string,
): Promise<MutationResult<RunnerRegistration>> {
  const result = await postJson<unknown>(runnerRegistrationTokenPath(target), {}, csrfToken);
  if (!result.ok) return result;
  const registration = normalizeRunnerRegistration(result.data);
  return registration ? { ok: true, data: registration } : { ok: false, kind: "unavailable", code: "invalid_response" };
}

export function revokeRunner(target: RunnerTarget, runnerID: string, csrfToken: string): Promise<MutationResult<null>> {
  return deleteJson(runnerPath(target, runnerID), csrfToken);
}
