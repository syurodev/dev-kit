import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { ReactFlowProvider, type NodeProps } from "@xyflow/react";
import { ApiNode, type ApiNodeData } from "./ApiNode";

function render(data: ApiNodeData) {
  const props = { id: "n", data, selected: false } as unknown as NodeProps & { data: ApiNodeData };
  return renderToStaticMarkup(
    <ReactFlowProvider>
      <ApiNode {...props} />
    </ReactFlowProvider>,
  );
}

describe("ApiNode", () => {
  test("a known layer with a summary, effects and hidden calls", () => {
    const html = render({
      layer: "service",
      title: "placeOrder",
      caption: "OrderService",
      titleTooltip: "com.shop.OrderService.placeOrder(Order)",
      detail: "OrderService.java:42",
      detailTooltip: "src/main/java/com/shop/OrderService.java:42",
      summary: "Places an order and tells billing.",
      effects: ["Kafka produce", "HTTP call"],
      hiddenCalls: 3,
    });
    expect(html).toContain("SERVICE");
    expect(html).toContain("OrderService");
    expect(html).toContain("placeOrder");
    // The summary takes the file chip's place; the file stays in its tooltip.
    expect(html).toContain("Places an order and tells billing.");
    expect(html).not.toContain(">OrderService.java:42<");
    expect(html).toContain('title="src/main/java/com/shop/OrderService.java:42"');
    expect(html).toContain("Kafka produce");
    expect(html).toContain("HTTP call");
    expect(html).toContain("+3 hidden calls");
  });

  test("an unknown layer says nothing about it, and no summary shows the file", () => {
    const html = render({ layer: "", title: "configure", detail: "Routes.java:7" });
    expect(html).not.toContain("UNKNOWN");
    expect(html).toContain("configure");
    expect(html).toContain(">Routes.java:7<");
    expect(html).not.toContain("hidden call");
  });

  test("one hidden call is singular", () => {
    expect(render({ layer: "controller", title: "get", detail: "A.java:1", hiddenCalls: 1 })).toContain("+1 hidden call<");
  });
});
