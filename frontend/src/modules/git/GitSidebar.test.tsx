import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { gitModule } from "./manifest";
import { GitSidebar } from "./GitSidebar";

describe("GitSidebar", () => {
  test("with no folder open it says so instead of drawing empty headings", () => {
    const html = renderToString(<GitSidebar />);
    expect(html).toContain("No folder open");
    expect(html).not.toContain("Repositories");
  });

  test("is the module's rail, so it is there with no tab open", () => {
    expect(gitModule.sidebar).toBeDefined();
  });
});
