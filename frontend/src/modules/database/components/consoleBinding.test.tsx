import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { ConsoleBinding } from "./ConsoleBinding";

const render = (props: Parameters<typeof ConsoleBinding>[0]) =>
  renderToString(<ConsoleBinding {...props} />);

/**
 * Where the statements go, said in the toolbar that runs them.
 *
 * The console is a file now, and a file that says "DELETE FROM users" says
 * nothing about which users. The header line in the file records it; this is
 * the same fact in the window.
 */
describe("what the console says it is pointed at", () => {
  it("names the connection and the database", () => {
    const html = render({ connection: "prod", database: "analytics", engine: "postgres" });
    expect(html).toContain("prod");
    expect(html).toContain("analytics");
  });

  it("says so plainly when there is nothing to run against", () => {
    const html = render({ connection: "", database: "" });
    expect(html).toContain("No connection selected");
  });

  /*
   * The live target, never the recorded one. A console restored from disk was
   * written against whatever was selected last session, and of the two facts
   * only the live one answers where the next Run lands. The recorded one is
   * shown beside it, because a query written for one database and about to be
   * sent to another is worth a second's pause.
   */
  it("shows the connection the file was written for when it is not this one", () => {
    const html = render({ connection: "staging", database: "shop", writtenFor: "prod" });
    const live = html.indexOf("staging");
    const recorded = html.indexOf("written for prod");
    expect(live).toBeGreaterThan(-1);
    expect(recorded).toBeGreaterThan(live);
  });

  it("says nothing at all when the file agrees with the selection", () => {
    const html = render({ connection: "prod", database: "shop" });
    expect(html).not.toContain("written for");
  });
});
