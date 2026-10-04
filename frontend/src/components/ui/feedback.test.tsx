import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { InlineFeedback, FeedbackBanner, PendingStatus } from "./feedback";

test("errors announce as alerts; progress and success announce politely", () => {
  expect(renderToStaticMarkup(<InlineFeedback>Save failed</InlineFeedback>)).toContain('role="alert"');
  expect(renderToStaticMarkup(<InlineFeedback tone="success">Copied</InlineFeedback>)).toContain('role="status"');
  expect(renderToStaticMarkup(<PendingStatus>Connecting</PendingStatus>)).toContain('role="status"');
  const banner = renderToStaticMarkup(<FeedbackBanner tone="warning" title="Disconnected">Reconnect to continue</FeedbackBanner>);
  expect(banner).toContain('role="status"');
  expect(banner).toContain("h-auto min-h-0");
});
