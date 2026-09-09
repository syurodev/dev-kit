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

  it("mounts only the active panel, inside a box the slide can clip against", () => {
    expect(html).toContain("panel-b");
    expect(html).not.toContain("panel-a");
    expect(html).toContain('data-slot="tabs-content"');
    // The leaving panel goes absolute against the root and both travel past
    // its sides — without these two the slide either jumps or spills.
    expect(html).toMatch(/data-slot="tabs" class="[^"]*\brelative\b[^"]*overflow-x-clip/);
  });

  it("puts exactly one sliding indicator, on the active trigger", () => {
    expect(html.match(/data-slot="tabs-indicator"/g)?.length).toBe(1);
    expect(html.indexOf('data-slot="tabs-indicator"')).toBeGreaterThan(html.indexOf(">A<") > 0 ? html.indexOf(">A<") : 0);
  });
});
