import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { VaultPasswordField } from "./VaultStage";

test("vault password field associates its label, error and password manager hint", () => {
  const html = renderToString(<VaultPasswordField label="Master password" value="" onChange={() => {}} placeholder="Password" invalid creating />);
  expect(html).toContain('type="password"');
  expect(html).toContain('autoComplete="new-password"');
  expect(html).toContain('aria-invalid="true"');
  expect(html).toContain('aria-describedby="vault-form-error"');
  const id = html.match(/<input id="([^"]+)"/)?.[1];
  expect(id).toBeDefined();
  expect(html).toContain(`for="${id}"`);
  expect(html).toContain('aria-label="Show master password"');
  expect(html).not.toContain('tabindex="-1"');
});

test("submitting disables password editing and reveal", () => {
  const html = renderToString(<VaultPasswordField label="Master password" value="" onChange={() => {}} placeholder="Password" disabled />);
  expect(html.match(/disabled=""/g)).toHaveLength(2);
  expect(html).toContain('autoComplete="current-password"');
});
