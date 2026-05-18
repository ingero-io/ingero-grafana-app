import { test, expect } from '@grafana/plugin-e2e';

/**
 * Multi-instance + per-query-type e2e against the fake-Echo
 * harness (tests/fake-echo/). Two datasource instances are
 * provisioned at boot via provisioning/datasources/ingero.yaml:
 *
 *   - "Ingero echo-a" → http://fake-echo-a:8081 (label = echo-a)
 *   - "Ingero echo-b" → http://fake-echo-b:8081 (label = echo-b)
 *
 * Every fake-Echo response carries an `_echo_label` field so each
 * test below asserts on the rendered value to verify which fake-
 * Echo answered.
 *
 * Selectors use plain CSS `#id` against the input/textarea
 * directly. @grafana/ui Input / SecretInput / TextArea components
 * forward the `id` prop to their underlying element; getByTestId
 * and getByRole (name=label) both fail because the wrapping
 * InlineField does NOT preserve data-testid on wrappers and does
 * NOT set aria-labelledby on the input.
 */

import type { Locator, Page } from '@playwright/test';

const ECHO_ENDPOINT = 'http://fake-echo-a:8081';

// Open a Grafana Select via its data-testid wrapper and click the
// named option. Grafana's Select renders each option's title +
// description into a single ARIA accessible name (e.g.
//   "SQL\nRun a read-only DuckDB query against Echo via POST /api/v2/sql."
// ), so exact: true on just the title never matches. Substring
// match works because every QUERY_TYPES title prefix is unique.
async function pickOption(page: Page, testId: string, label: string) {
  await page.getByTestId(testId).click();
  await page.getByRole('option', { name: label }).first().click();
}

test('config: Save & test green against fake-echo-a', async ({
  createDataSourceConfigPage,
  page,
}) => {
  const configPage = await createDataSourceConfigPage({ type: 'ingero-gpu-datasource' });
  await page.locator('#config-editor-endpoint').fill(ECHO_ENDPOINT);
  await page.locator('#config-editor-bearer').fill('dev-bearer');
  await expect(configPage.saveAndTest()).toBeOK();
});

test('config: Save & test fails with bad bearer', async ({
  createDataSourceConfigPage,
  page,
}) => {
  const configPage = await createDataSourceConfigPage({ type: 'ingero-gpu-datasource' });
  await page.locator('#config-editor-endpoint').fill(ECHO_ENDPOINT);
  await page.locator('#config-editor-bearer').fill('not-the-right-bearer');
  await expect(configPage.saveAndTest()).not.toBeOK();
});

test('query: SQL returns rows', async ({ panelEditPage, page }) => {
  await panelEditPage.datasource.set('Ingero echo-a');
  // Fake-Echo returns all-string columns; Grafana 12.3.x default
  // viz is "Time series" which rejects no-number frames with
  // "Data is missing a number field". Table accepts any frame
  // shape. Set explicitly for cross-version stability.
  await panelEditPage.setVisualization('Table');
  await pickOption(page, 'query-editor-type', 'SQL');
  await page.locator('#query-editor-sql').fill('SELECT * FROM events LIMIT 5');
  await expect(panelEditPage.refreshPanel()).toBeOK();
  await expect(panelEditPage.panel.data.getByText('node-a').first()).toBeVisible();
});

test('query: MCP tool returns scripted rows with echo-a label', async ({
  panelEditPage,
  page,
}) => {
  await panelEditPage.datasource.set('Ingero echo-a');
  await panelEditPage.setVisualization('Table');
  await pickOption(page, 'query-editor-type', 'MCP tool');
  // Tool picker allows custom values via onCreateOption: open, type,
  // Enter to commit. The wrapper div around the Select carries
  // data-testid="query-editor-tool".
  await page.getByTestId('query-editor-tool').click();
  await page.keyboard.type('fleet.cluster.summary');
  await page.keyboard.press('Enter');
  await page.locator('#query-editor-tool-args').fill('{}');
  await expect(panelEditPage.refreshPanel()).toBeOK();
  await expect(panelEditPage.panel.data.getByText('echo-a').first()).toBeVisible();
});

test('query: anomaly stream returns rows from cluster.anomaly_list', async ({
  panelEditPage,
  page,
}) => {
  await panelEditPage.datasource.set('Ingero echo-a');
  await panelEditPage.setVisualization('Table');
  await pickOption(page, 'query-editor-type', 'Anomaly stream');
  await page.locator('#query-editor-time-window').fill('1h');
  await expect(panelEditPage.refreshPanel()).toBeOK();
  await expect(panelEditPage.panel.data.getByText('echo-a').first()).toBeVisible();
});

test('multi-instance: query against echo-b carries echo-b label', async ({
  panelEditPage,
  page,
}) => {
  // Same query type, different datasource: the panel surfaces a
  // different _echo_label, proving the multi-instance switch
  // reaches the second fake-Echo container.
  await panelEditPage.datasource.set('Ingero echo-b');
  await panelEditPage.setVisualization('Table');
  await pickOption(page, 'query-editor-type', 'MCP tool');
  await page.getByTestId('query-editor-tool').click();
  await page.keyboard.type('fleet.cluster.summary');
  await page.keyboard.press('Enter');
  await page.locator('#query-editor-tool-args').fill('{}');
  await expect(panelEditPage.refreshPanel()).toBeOK();
  await expect(panelEditPage.panel.data.getByText('echo-b').first()).toBeVisible();
});

test('client-side validation: invalid tool name is rejected before dispatch', async ({
  panelEditPage,
  page,
}) => {
  await panelEditPage.datasource.set('Ingero echo-a');
  await pickOption(page, 'query-editor-type', 'MCP tool');
  await page.getByTestId('query-editor-tool').click();
  // The tool Select is allowCustomValue + populates options from
  // /resources/tools. The fake-Echo exposes `fleet.cluster.summary`
  // and `fleet.cluster.anomaly_list`. We want a typed value that
  // (a) does NOT case-insensitively match any existing option (else
  // Select auto-picks the existing match), and (b) is outside the
  // backend regex ^[a-z][a-z0-9_.]{1,127}$. A slash satisfies both:
  // no real tool has one, and the regex forbids it.
  await page.keyboard.type('bad/tool/name');
  await page.keyboard.press('Enter');
  await page.locator('#query-editor-tool-args').fill('{}');
  await panelEditPage.refreshPanel();
  // Backend returns a structured error; Grafana surfaces it in the
  // panel's error overlay (not its data cells). Look anywhere on
  // the panel-edit page for the substring.
  await expect(page.getByText(/invalid tool name/i).first()).toBeVisible();
});
