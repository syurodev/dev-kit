import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { ResponsePane } from "./ResponsePane";
import { KeyValueTable } from "./KeyValueTable";

const response = { status: 200, statusText: "200 OK", headers: {}, body: "sample", truncated: false, bytes: 6, durationMs: 42 };
const props = { response, sending: false, error: "", curl: "", onCloseCurl() {}, requestId: "", sends: 1 };

test("keeps the previous response identifiable while sending and after a failed retry", () => {
  const pending = renderToString(<ResponsePane {...props} sending />);
  expect(pending).toContain("Previous response");
  expect(pending).toContain("200 OK");
  expect(pending).toContain('role="status"');
  const failed = renderToString(<ResponsePane {...props} error="Connection refused" />);
  expect(failed).toContain("Previous response");
  expect(failed).toContain('role="alert"');
  expect(failed).toContain("Connection refused");
});

test("Pretty and Raw expose which representation is selected", () => {
  const html = renderToString(<ResponsePane {...props} />);
  expect(html).toContain('aria-pressed="true"');
  expect(html).toContain('aria-pressed="false"');
  expect(html).not.toContain("Previous response");
});

test("secret rows stay masked and their toggle has an accessible name", () => {
  const html = renderToString(<KeyValueTable rows={[{ key: "token", value: "" }]} secrets={[true]} onChange={() => {}} onToggleSecret={() => {}} />);
  expect(html).toContain('type="password"');
  expect(html).toContain('aria-label="Make plain: token"');
  expect(html).toContain('aria-pressed="true"');
});
