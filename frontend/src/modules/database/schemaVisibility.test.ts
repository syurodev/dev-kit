import { expect, test } from "bun:test";
import { visibleNamespace, toggleVisibleSchema } from "./schemaVisibility";
const namespace = { public: { orders: ["id"] }, billing: { invoices: ["id"] }, Public: {} };
test("all mode includes newly discovered schemas; none stays explicitly empty", () => {
  expect(visibleNamespace(namespace, null)).toBe(namespace);
  expect(visibleNamespace(namespace, [])).toEqual({});
  expect(visibleNamespace(namespace, ["public", "removed"])).toEqual({ public: namespace.public });
});
test("checkboxes respect case-sensitive schema identity and preserve catalog order", () => {
  expect(toggleVisibleSchema(Object.keys(namespace), null, "public", false)).toEqual(["billing", "Public"]);
  expect(toggleVisibleSchema(Object.keys(namespace), ["billing"], "public", true)).toEqual(["public", "billing"]);
  expect(visibleNamespace(namespace, ["Public"])).toEqual({ Public: {} });
});
