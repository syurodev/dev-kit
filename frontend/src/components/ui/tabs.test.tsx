import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./tabs";

describe("Tabs", () => {
  const html = renderToString(
    <Tabs defaultValue="b">
      <TabsList>
        <TabsTrigger value="a">A</TabsTrigger>
        <TabsTrigger value="b">B</TabsTrigger>
      </TabsList>
      <TabsContent value="a">panel-a</TabsContent>
      <TabsContent value="b">panel-b</TabsContent>
    </Tabs>,
  );

  it("mounts only the shown panel, wired for the swap transition", () => {
    expect(html).toContain("panel-b");
    expect(html).not.toContain("panel-a");
    expect(html).toContain("tabs-content-swap");
    expect(html).not.toContain("data-leaving");
  });

  it("puts exactly one sliding indicator, on the active trigger", () => {
    expect(html.match(/data-slot="tabs-indicator"/g)?.length).toBe(1);
    expect(html.indexOf('data-slot="tabs-indicator"')).toBeGreaterThan(html.indexOf(">A<") > 0 ? html.indexOf(">A<") : 0);
  });
});
