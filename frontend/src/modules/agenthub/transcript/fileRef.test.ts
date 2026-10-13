import { describe, expect, test } from "bun:test";
import { fileRef } from "./fileRef";

describe("fileRef", () => {
  test("paths with a source extension", () => {
    expect(fileRef("auth/session.go:88")).toEqual({ path: "auth/session.go", line: 88 });
    expect(fileRef("src/App.tsx:12:4")).toEqual({ path: "src/App.tsx", line: 12 });
    expect(fileRef("internal/agenthub/service.go")).toEqual({ path: "internal/agenthub/service.go" });
    expect(fileRef("/abs/path/main.go")).toEqual({ path: "/abs/path/main.go" });
    expect(fileRef("README.md")).toEqual({ path: "README.md" });
  });
  test("not paths", () => {
    for (const s of ["https://x.test/a.go", "SessionTTL", "a b/c.go", "v1.2.3", "x.y", "npm run build", "1.5", "../"]) {
      expect(fileRef(s)).toBeNull();
    }
  });
});
