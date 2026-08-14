import { expect, test } from "@playwright/test";

test.describe.serial("LoreHub browser smoke test", () => {
  test("public pages render in English and Japanese", async ({ page }) => {
    await page.goto("/en");
    await expect(page).toHaveTitle(/LoreHub/u);
    await expect(page.getByRole("heading", { level: 1, name: "Explore" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Sign up" })).toBeVisible();

    await page.goto("/ja");
    await expect(page.locator("html")).toHaveAttribute("lang", "ja");
    await expect(page.getByRole("heading", { level: 1, name: "探す" })).toBeVisible();
  });

  test("a new user can create an organization, repository, and issue", async ({ page }) => {
    const suffix = `${Date.now()}-${test.info().retry}`;
    const email = `browser-${suffix}@example.test`;
    const password = "Browser-Test-42!";
    const organization = `browser-${suffix}`;
    const repository = "smoke";
    const issueTitle = `Browser smoke ${suffix}`;

    await page.goto("/en/auth/register?return_to=%2Fen%2Forganizations%2Fnew");
    await page.getByRole("link", { name: "Continue with email and password" }).click();
    await expect(page).toHaveURL(/\/realms\/lorehub\//u);

    await page.locator("#email").fill(email);
    await page.locator("#firstName").fill("Browser");
    await page.locator("#lastName").fill("Test");
    await page.locator("#password").fill(password);
    await page.locator("#password-confirm").fill(password);
    await page.locator('input[type="submit"]').click();

    await expect(page).toHaveURL(/\/en\/organizations\/new$/u);
    await page.locator("#organization-slug").fill(organization);
    await page.locator("#organization-name").fill("Browser smoke organization");
    await page.getByRole("button", { name: "Create organization" }).click();
    await expect(page).toHaveURL(new RegExp(`/en/organizations/${organization}$`, "u"));

    await page.goto("/en/repositories/new");
    await page.locator("#repository-organization").selectOption(organization);
    await page.locator("#repository-slug").fill(repository);
    await page.locator("#repository-name").fill("Browser smoke repository");
    await page.getByRole("button", { name: "Create repository" }).click();
    await expect(page).toHaveURL(new RegExp(`/en/${organization}/${repository}$`, "u"));

    await page.goto(`/en/${organization}/${repository}/issues/new`);
    await page.locator("#issue-title").fill(issueTitle);
    await page.locator("#issue-body").fill("Created by the browser smoke test.");
    await page.getByRole("button", { name: "Submit new issue" }).click();

    await expect(page).toHaveURL(new RegExp(`/en/${organization}/${repository}/issues\\?created=1$`, "u"));
    await expect(page.getByRole("link", { name: issueTitle })).toBeVisible();
  });
});
