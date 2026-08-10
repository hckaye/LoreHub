import type { APIResult, CodeScanningAlert, SARIFUploadMetadata } from "./api-types";

export type CodeScanningViewState = "ready" | "empty" | "forbidden" | "unavailable";

export type CodeScanningAlertRow = {
  alert: CodeScanningAlert;
  upload: SARIFUploadMetadata | null;
};

export function codeScanningViewState(
  uploads: APIResult<SARIFUploadMetadata[]>,
  alerts: APIResult<CodeScanningAlert[]>,
): CodeScanningViewState {
  if (isPermissionFailure(uploads) || isPermissionFailure(alerts)) {
    return "forbidden";
  }
  if (!uploads.ok || !alerts.ok) {
    return "unavailable";
  }
  return alerts.data.length === 0 ? "empty" : "ready";
}

export function codeScanningAlertRows(
  alerts: CodeScanningAlert[],
  uploads: SARIFUploadMetadata[],
): CodeScanningAlertRow[] {
  const uploadsByID = new Map(uploads.map((upload) => [upload.id, upload]));
  return alerts.map((alert) => ({ alert, upload: uploadsByID.get(alert.uploadId) ?? null }));
}

function isPermissionFailure(result: APIResult<unknown>): boolean {
  return (
    !result.ok && (result.reason === "unauthorized" || result.reason === "forbidden" || result.reason === "not-found")
  );
}
