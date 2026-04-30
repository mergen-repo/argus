// SKIPPED_NEEDS_PLAYWRIGHT_SETUP
// Playwright is not yet configured for this project.
// To enable: npm install -D @playwright/test && npx playwright install
// Then update web/package.json with a "test:e2e" script and create playwright.config.ts.

import { test, expect } from "@playwright/test";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://localhost:8084";

async function login(page: import("@playwright/test").Page) {
  await page.goto(`${BASE_URL}/login`);
  await page.fill('input[type="email"]', "admin@argus.io");
  await page.fill('input[type="password"]', "admin");
  await page.click('button[type="submit"]');
  await page.waitForURL(`${BASE_URL}/dashboard`, { timeout: 10000 });
}

test.describe("SLA Historical Page", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test.skip("navigates to /sla and renders 6 month cards", async ({ page }) => {
    await page.goto(`${BASE_URL}/sla`);
    const cards = page.locator("[data-testid='sla-month-card']");
    await expect(cards).toHaveCount(6);
    const firstCard = cards.first();
    await expect(firstCard.locator("[data-testid='uptime-pct']")).toBeVisible();
    await expect(
      firstCard.locator("[data-testid='incident-count']")
    ).toBeVisible();
  });

  test.skip("clicking a month card opens SlidePanel with operators table", async ({
    page,
  }) => {
    await page.goto(`${BASE_URL}/sla`);
    const firstCard = page.locator("[data-testid='sla-month-card']").first();
    await firstCard.click();
    const panel = page.locator("[data-testid='month-detail-panel']");
    await expect(panel).toBeVisible({ timeout: 5000 });
    await expect(
      panel.locator("[data-testid='operator-row']").first()
    ).toBeVisible();
  });

  test.skip("clicking operator row 'View breaches' opens nested breach SlidePanel", async ({
    page,
  }) => {
    await page.goto(`${BASE_URL}/sla`);
    await page.locator("[data-testid='sla-month-card']").first().click();
    const panel = page.locator("[data-testid='month-detail-panel']");
    await expect(panel).toBeVisible({ timeout: 5000 });
    const viewBreachesBtn = panel
      .locator("[data-testid='view-breaches-btn']")
      .first();
    await viewBreachesBtn.click();
    const breachPanel = page.locator("[data-testid='breach-list-panel']");
    await expect(breachPanel).toBeVisible({ timeout: 5000 });
    await expect(breachPanel.locator("[data-testid='breach-row']")).toHaveCount(
      { minimum: 1 }
    );
  });

  test.skip("PDF link href matches /api/v1/sla/pdf?year=...&month=... and returns 200 application/pdf", async ({
    page,
    request,
  }) => {
    await page.goto(`${BASE_URL}/sla`);
    const firstCard = page.locator("[data-testid='sla-month-card']").first();
    const pdfLink = firstCard.locator("[data-testid='pdf-link']");
    const href = await pdfLink.getAttribute("href");
    expect(href).toMatch(/\/api\/v1\/sla\/pdf\?year=\d{4}&month=\d{1,2}/);

    const cookies = await page.context().cookies();
    const cookieHeader = cookies
      .map((c) => `${c.name}=${c.value}`)
      .join("; ");
    const response = await request.get(`${BASE_URL}${href}`, {
      headers: { Cookie: cookieHeader },
    });
    expect(response.status()).toBe(200);
    expect(response.headers()["content-type"]).toContain("application/pdf");
  });

  test.skip("Operator Detail — SLA Targets section: update uptime target, save, persist on reload", async ({
    page,
  }) => {
    await page.goto(`${BASE_URL}/operators`);
    await page.locator("[data-testid='operator-row']").first().click();
    await page.locator("[data-testid='tab-protocols']").click();
    const slaSection = page.locator("[data-testid='sla-targets-section']");
    await expect(slaSection).toBeVisible({ timeout: 5000 });

    const uptimeInput = slaSection.locator(
      "input[name='sla_uptime_target'], input[aria-label='Uptime Target']"
    );
    await uptimeInput.clear();
    await uptimeInput.fill("99.95");
    await slaSection.locator("[data-testid='sla-save-btn']").click();

    await expect(page.locator("[data-testid='toast-success']")).toBeVisible({
      timeout: 5000,
    });

    await page.reload();
    await page.locator("[data-testid='tab-protocols']").click();
    const persistedValue = await page
      .locator(
        "[data-testid='sla-targets-section'] input[name='sla_uptime_target'], [data-testid='sla-targets-section'] input[aria-label='Uptime Target']"
      )
      .inputValue();
    expect(persistedValue).toBe("99.95");
  });
});
