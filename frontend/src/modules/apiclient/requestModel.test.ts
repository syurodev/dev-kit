import { describe, expect, it } from "bun:test";
import {
  parseUrlBar,
  pathVarNames,
  referencedVars,
  sameRequest,
  syncPathVars,
  urlBarText,
  emptyRequest,
  prettyBody,
} from "./requestModel";
import { buildVarsUpdate, rowsFromVarSet } from "./variablesModel";
import { buildSidebarTree } from "./sidebarTree";
import type { ApiCollection, ApiRequest } from "@/bridge/apiclient";

describe("the URL bar and the Params table are one request", () => {
  it("shows the enabled rows as a query, unencoded", () => {
    expect(
      urlBarText("{{baseUrl}}/users", [
        { key: "q", value: "a b" },
        { key: "off", value: "1", disabled: true },
        { key: "flag", value: "" },
      ]),
    ).toBe("{{baseUrl}}/users?q=a b&flag");
  });

  it("reads typing back into rows and keeps the switched-off ones", () => {
    const parsed = parseUrlBar("https://x.test/a?ids=1&ids=2&q=a%20b", [
      { key: "ids", value: "9" },
      { key: "off", value: "1", disabled: true },
    ]);
    expect(parsed.url).toBe("https://x.test/a");
    expect(parsed.query).toEqual([
      { key: "ids", value: "1" },
      { key: "ids", value: "2" },
      { key: "q", value: "a b" },
      { key: "off", value: "1", disabled: true },
    ]);
  });

  it("round-trips a URL with no query", () => {
    expect(parseUrlBar("https://x.test/a", [])).toEqual({ url: "https://x.test/a", query: [] });
  });
});

describe("path variables", () => {
  it("are the :segments of the path, not of the host or the query", () => {
    expect(pathVarNames("http://localhost:8080/users/:id/repos/:repo?x=:no")).toEqual(["id", "repo"]);
    expect(pathVarNames("{{baseUrl}}/a/:id")).toEqual(["id"]);
  });

  it("keep the values typed and drop the rows whose segment is gone", () => {
    expect(syncPathVars("/a/:id/:other", [{ key: "id", value: "7" }, { key: "gone", value: "x" }])).toEqual([
      { key: "id", value: "7" },
      { key: "other", value: "" },
    ]);
  });
});

describe("unsaved changes", () => {
  it("ignores the blank rows an editor adds and the body of a request with none", () => {
    const saved = { ...emptyRequest(), name: "x", url: "https://x.test", body: "" };
    expect(sameRequest(saved, { ...saved, headers: [{ key: "", value: "" }] })).toBe(true);
    expect(sameRequest(saved, { ...saved, body: "left over", bodyMode: "none" })).toBe(true);
    expect(sameRequest(saved, { ...saved, method: "POST" })).toBe(false);
  });

  it("finds every {{variable}} a request uses", () => {
    const request = {
      ...emptyRequest(),
      url: "{{baseUrl}}/x",
      headers: [{ key: "X-Tenant", value: "{{tenant}}" }, { key: "Off", value: "{{off}}", disabled: true }],
      body: '{"a":"{{a}}"}',
    };
    expect(referencedVars(request).sort()).toEqual(["a", "baseUrl", "tenant"]);
  });

  it("indents JSON and leaves anything else alone", () => {
    expect(prettyBody('{"a":1}', "json")).toBe('{\n  "a": 1\n}');
    expect(prettyBody("not json", "json")).toBe("not json");
  });
});

describe("variables", () => {
  it("keeps a sealed secret left empty, replaces one given a value, drops one renamed", () => {
    const rows = rowsFromVarSet({ vars: { baseUrl: "https://x" }, secretVars: ["keep", "replace", "rename"] });
    const edited = rows.map((row) =>
      row.key === "replace" ? { ...row, value: "new" } : row.key === "rename" ? { ...row, key: "renamed" } : row,
    );
    expect(buildVarsUpdate(edited)).toEqual({
      vars: { baseUrl: "https://x" },
      secrets: { replace: "new", renamed: "" },
      keepSecrets: ["keep"],
    });
  });
});

describe("the rail", () => {
  const collections: ApiCollection[] = [
    { id: "c1", name: "Shop", order: 0 },
    { id: "c2", name: "Orders", parentId: "c1", order: 0 },
    { id: "orphan", name: "Lost", parentId: "deleted", order: 0 },
  ];
  const request = (id: string, name: string, collectionId: string): ApiRequest => ({
    ...emptyRequest(collectionId),
    id,
    name,
    url: `https://x.test/${name}`,
    createdAt: "",
    updatedAt: "",
  });
  const requests = [request("r1", "list orders", "c2"), request("r2", "health", ""), request("r3", "cart", "c1")];

  it("nests folders, counts what is below, and puts unfiled requests at the end", () => {
    const tree = buildSidebarTree(collections, requests);
    expect(tree.map((entry) => (entry.kind === "folder" ? entry.collection.name : entry.request.name))).toEqual([
      "Lost",
      "Shop",
      "health",
    ]);
    const shop = tree[1];
    expect(shop.kind === "folder" && shop.total).toBe(2);
  });

  it("searches requests and keeps the folders on the way to them", () => {
    const tree = buildSidebarTree(collections, requests, "orders");
    expect(tree).toHaveLength(1);
    const shop = tree[0];
    expect(shop.kind === "folder" && shop.children.map((c) => c.kind)).toEqual(["folder"]);
  });
});
