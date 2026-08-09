export const MAX_TITLE_LENGTH = 512;
export const MAX_BODY_LENGTH = 1_000_000;

export type FormValidationError = "titleRequired" | "titleTooLong" | "bodyTooLong" | "branchesSame";

export function validateTitleAndBody(title: string, body: string): FormValidationError[] {
  const errors: FormValidationError[] = [];
  const trimmedTitle = title.trim();
  if (!trimmedTitle) {
    errors.push("titleRequired");
  } else if (trimmedTitle.length > MAX_TITLE_LENGTH) {
    errors.push("titleTooLong");
  }
  if (body.length > MAX_BODY_LENGTH) {
    errors.push("bodyTooLong");
  }
  return errors;
}

export function validateBranchSelection(sourceBranch: string, targetBranch: string): FormValidationError[] {
  return sourceBranch === targetBranch ? ["branchesSame"] : [];
}
