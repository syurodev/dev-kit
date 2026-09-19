import { describe, expect, it } from "bun:test";
import { dedent } from "./dedent";

describe("dedent", () => {
  it("removes the indentation a method carries from its class", () => {
    const method = ["    List<DeviceSummary> list(", "            ListDevicesCommand command);"].join("\n");
    expect(dedent(method)).toBe(["List<DeviceSummary> list(", "        ListDevicesCommand command);"].join("\n"));
  });

  it("keeps every line where it was relative to the others", () => {
    const source = ["        if (ok) {", "            run();", "        }"].join("\n");
    expect(dedent(source)).toBe(["if (ok) {", "    run();", "}"].join("\n"));
  });

  it("does not let a blank line decide, and empties it", () => {
    const source = ["    a();", "", "  \t ", "    b();"].join("\n");
    expect(dedent(source)).toBe(["a();", "", "", "b();"].join("\n"));
  });

  it("leaves code that already starts at the margin alone", () => {
    const source = ["class Foo {", "    int x;", "}"].join("\n");
    expect(dedent(source)).toBe(source);
  });

  it("shares only what tabs and spaces actually have in common", () => {
    // One line indents with a tab, the other with spaces: nothing is shared,
    // so nothing moves — guessing a width here could misalign them.
    const mixed = ["\tfoo();", "    bar();"].join("\n");
    expect(dedent(mixed)).toBe(mixed);

    const tabs = ["\t\tfoo();", "\t\t\tbar();"].join("\n");
    expect(dedent(tabs)).toBe(["foo();", "\tbar();"].join("\n"));
  });

  it("handles a single line and an empty string", () => {
    expect(dedent("    return x;")).toBe("return x;");
    expect(dedent("")).toBe("");
  });
});
