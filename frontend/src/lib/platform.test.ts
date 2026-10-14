import { describe, expect, test } from "bun:test";
import { quietsScrollBoxes } from "./platform";

describe("quieting scroll boxes while a shell panel is inert", () => {
  test("only in the Linux desktop app (WebKitGTK), and only with overlay scrollbars", () => {
    // WebKitGTK rebuilds a clipped scroller's layer in the frames that reveal it.
    expect(quietsScrollBoxes("linux", 0)).toBe(true);
    // A scrollbar that takes width would vanish and the content re-wrap when inert lifts.
    expect(quietsScrollBoxes("linux", 8)).toBe(false);
    // WebView2, WKWebView and plain browsers keep their scroll boxes.
    expect(quietsScrollBoxes("windows", 0)).toBe(false);
    expect(quietsScrollBoxes("mac", 0)).toBe(false);
    expect(quietsScrollBoxes("web", 0)).toBe(false);
  });
});
