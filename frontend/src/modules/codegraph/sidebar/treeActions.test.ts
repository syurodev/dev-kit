import { expect, it } from "bun:test";
import type { CodeGraphEntryPoint } from "@/bridge/codegraph";
import { entryCollapseKeys, fileAncestorKeys } from "./treeActions";
import { treeDirId } from "../workspace/identity";

it("reveals every possible compact directory prefix in the owning folder", () => {
  expect(fileAncestorKeys("db", "liquibase/changelog/billing/v01.yaml")).toEqual([
    treeDirId("db", "liquibase"), treeDirId("db", "liquibase/changelog"), treeDirId("db", "liquibase/changelog/billing"),
  ]);
  expect(fileAncestorKeys("db", "Makefile")).toEqual([]);
});

it("collapses roots, kinds, services and owners independently across folders", () => {
  const entry = (kind: string, service: string, owner: string): CodeGraphEntryPoint => ({
    id: `${kind}:${service}:${owner}`, entryNode: owner, kind, service, owner, group: owner,
    verb: "GET", target: "/health", order: 0,
  });
  const keys = entryCollapseKeys([
    { folderId: "a", entries: [entry("http", "api", "Health"), entry("message", "bus", "Events")] },
    { folderId: "b", entries: [entry("http", "api", "Health")] },
    { folderId: "empty", entries: [] },
  ]);
  expect(keys).toEqual(expect.arrayContaining([
    "folder:a", "a:kind:http", "a:svc:http:api", "a:own:http:Health",
    "a:kind:message", "a:svc:message:bus", "a:own:message:Events",
    "folder:b", "b:kind:http", "b:svc:http:api", "b:own:http:Health", "folder:empty",
  ]));
});
