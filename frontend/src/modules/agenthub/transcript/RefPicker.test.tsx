import { afterEach, describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { pickTarget, RefPickerList, settleRefPicker, useRefPickerStore } from "./RefPicker";
import type { RefTarget } from "./resolveFileRef";

const A: RefTarget = { workspaceId: "ws", folderId: "api", folderName: "api", rel: "kong/handler.lua" };
const B: RefTarget = { workspaceId: "ws", folderId: "gw", folderName: "gateway", rel: "handler.lua" };

afterEach(() => useRefPickerStore.setState({ hosts: [], anchorRect: null, targets: [], resolve: null }));

describe("pickTarget", () => {
  test("with no transcript mounted there is nobody to ask", async () => {
    expect(await pickTarget(null, [A, B])).toBeNull();
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
