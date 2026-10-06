import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { TypedValueInput } from "./TypedValueInput";
const render = (type: string, value: string | null, nullable = true) =>
  renderToStaticMarkup(
    <TypedValueInput
      type={type}
      value={value}
      nullable={nullable}
      onChange={() => {}}
    />,
  );
test("boolean aliases select a real boolean rather than a quoted database string", () => {
  expect(render("boolean", "t")).toContain(
    '<option value="true" selected="">true</option>',
  );
  expect(render("tinyint(1)", "0")).toContain(
    '<option value="false" selected="">false</option>',
  );
});
test("NULL is explicit and cannot be chosen on a NOT NULL column", () => {
  expect(render("text", null)).toContain(
    '<option value="null" selected="">SQL NULL</option>',
  );
  expect(render("text", "", false)).toContain(
    '<option value="null" disabled="">SQL NULL (not allowed)</option>',
  );
});
test("timestamp picker keeps milliseconds and an explicit offset without Date conversion", () => {
  const html = render("timestamptz", "2026-10-05 12:30:01.123+07:00");
  expect(html).toContain('type="datetime-local"');
  expect(html).toContain('value="2026-10-05T12:30:01.123"');
  expect(html).toContain('value="+07:00"');
});
test("database durations and infinity use text instead of an empty native picker", () => {
  expect(render("time", "-838:59:59.123456")).toContain('type="text"');
  expect(render("timestamp", "infinity")).toContain('value="infinity"');
});
test("large integers and decimals stay text and retain every digit", () => {
  expect(render("bigint", "90071992547409931234")).toContain(
    'value="90071992547409931234"',
  );
  expect(render("numeric(30,10)", "12345678901234567890.1234567890")).toContain(
    'value="12345678901234567890.1234567890"',
  );
});

test("sub-millisecond timestamps use exact text because WebKit pickers cannot represent them", () => {
  const html = render("timestamptz", "2026-10-05 12:30:01.123456+07:00");
  expect(html).toContain('type="text"');
  expect(html).toContain('value="2026-10-05 12:30:01.123456+07:00"');
  expect(html).toContain("Use text to preserve precision");
});
