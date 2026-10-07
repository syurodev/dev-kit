import { describe, expect, test } from "bun:test";
import { updateStatusLine, versionLabel, type UpdateStatus } from "./updates";

function status(over: Partial<UpdateStatus>): UpdateStatus {
  return {
    current: "1.0.0",
    skipped: "",
    latest: "1.0.0",
    available: false,
    prerelease: false,
    url: "",
    publishedAt: "",
    ...over,
  };
}

describe("versionLabel", () => {
  test("prefixes a release version and leaves the dev placeholder alone", () => {
    expect(versionLabel("1.4.0")).toBe("v1.4.0");
    expect(versionLabel("1.4.0-beta")).toBe("v1.4.0-beta");
    expect(versionLabel("dev")).toBe("dev");
  });
});

describe("updateStatusLine", () => {
  test("says nothing was compared for a development build", () => {
    expect(updateStatusLine(status({ current: "dev", skipped: "dev" }))).toContain("development build");
  });

  test("names the new version; a beta's own tag already says it is one", () => {
    expect(updateStatusLine(status({ available: true, latest: "1.1.0", url: "u" }))).toBe("v1.1.0 is available.");
    expect(
      updateStatusLine(status({ available: true, latest: "1.1.0-beta", prerelease: true, url: "u" })),
    ).toBe("v1.1.0-beta is available.");
  });

  test("reports being current when nothing newer exists", () => {
    expect(updateStatusLine(status({}))).toBe("You are on the latest version.");
  });

  test("distinguishes the reasons a check had nothing to report", () => {
    expect(updateStatusLine(status({ skipped: "disabled" }))).toBe("Automatic checks are off.");
    expect(updateStatusLine(status({ skipped: "no_release" }))).toBe("No release has been published yet.");
  });
});
