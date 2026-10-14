import { afterEach, describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { pickTarget, RefPickerList, settleRefPicker, useRefPickerStore } from "./RefPicker";
import type { RefTarget } from "./resolveFileRef";

const A: RefTarget = { workspaceId: "ws", folderId: "api", folderName: "api", rel: "kong/handler.lua" };
const B: RefTarget = { workspaceId: "ws", folderId: "gw", folderName: "gateway", rel: "handler.lua" };

afterEach(() => {
  settleRefPicker(null);
  useRefPickerStore.setState({ hosts: [] });
});

function fakeElement(opts: { connected?: boolean; rects?: { left: number; top: number; width: number; height: number }[]; viewport?: unknown } = {}) {
  const el = {
    isConnected: opts.connected ?? true,
    focused: 0,
    focus() {
      el.focused++;
    },
    getClientRects: () => opts.rects ?? [],
    getBoundingClientRect: () => ({ left: 1, top: 2, width: 300, height: 40 }),
    closest: () => opts.viewport ?? null,
  };
  return el;
}

describe("pickTarget", () => {
  test("with no transcript mounted there is nobody to ask", async () => {
    expect(await pickTarget(null, [A, B])).toBe("unavailable");
  });

  test("a wrapped reference anchors at its first line", () => {
    useRefPickerStore.setState({ hosts: ["h1"] });
    const el = fakeElement({ rects: [{ left: 200, top: 10, width: 80, height: 16 }, { left: 0, top: 26, width: 60, height: 16 }] });
    void pickTarget(el as unknown as HTMLElement, [A, B]);
    expect(useRefPickerStore.getState().anchorRect).toEqual({ left: 200, top: 10, width: 80, height: 16 });
  });

  test("settling gives focus back to the reference", async () => {
    useRefPickerStore.setState({ hosts: ["h1"] });
    const el = fakeElement();
    const answer = pickTarget(el as unknown as HTMLElement, [A, B]);
    settleRefPicker(A);
    expect(await answer).toEqual(A);
    expect(el.focused).toBe(1);
  });

  test("a reference that re-rendered away hands focus to the transcript", async () => {
    useRefPickerStore.setState({ hosts: ["h1"] });
    const viewport = fakeElement();
    const el = fakeElement({ viewport });
    const answer = pickTarget(el as unknown as HTMLElement, [A, B]);
    el.isConnected = false;
    settleRefPicker(null);
    expect(await answer).toBeNull();
    expect(el.focused).toBe(0);
    expect(viewport.focused).toBe(1);
  });

  test("opens the picker and resolves with the row picked", async () => {
    useRefPickerStore.setState({ hosts: ["h1"] });
    const answer = pickTarget(null, [A, B]);
    expect(useRefPickerStore.getState().targets).toEqual([A, B]);
    expect(useRefPickerStore.getState().anchorRect).not.toBeNull();
    settleRefPicker(B);
    expect(await answer).toEqual(B);
    expect(useRefPickerStore.getState().anchorRect).toBeNull();
  });

  test("a newer question dismisses an unanswered one", async () => {
    useRefPickerStore.setState({ hosts: ["h1"] });
    const first = pickTarget(null, [A, B]);
    const second = pickTarget(null, [B, A]);
    expect(await first).toBeNull();
    settleRefPicker(A);
    expect(await second).toEqual(A);
  });
});

describe("RefPickerList", () => {
  test("rows are buttons: the file name, then folder and directory", () => {
    const html = renderToString(<RefPickerList targets={[A, B]} onPick={() => {}} />);
    expect(html.match(/<button/g)?.length).toBe(2);
    expect(html).toContain(">handler.lua</span>");
    expect(html).toContain("api · kong");
    // A file at a folder's root names only the folder.
    expect(html).toContain(">gateway</span>");
  });
});
