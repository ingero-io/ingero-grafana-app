import { test, expect } from './fixtures';

// Smoke: the app's overview page renders without crashing and the
// "Ingero" heading is present. The richer multi-instance + MCP-panel
// scenarios land in the D9 fake-Echo harness suite; this single test
// is the minimum-viable gate that protects against an app-side
// regression breaking the catalog listing.
test('overview page renders', async ({ page, gotoPage }) => {
  await gotoPage('/');
  await expect(page.getByRole('heading', { level: 2, name: 'Ingero' })).toBeVisible();
});
