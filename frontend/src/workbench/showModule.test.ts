import { beforeEach, describe, expect, test } from "bun:test";
import { Package } from "lucide-react";
import { registerModule, type ModuleManifest } from "@/modules/registry";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { openHomeWhenAsked } from "./showModule";

function fake(id: string, opensHome: boolean): ModuleManifest {
  return {
    id,
    title: id,
    version: "1.0.0",
    apiVersion: "1",
    icon: Package,
    capabilities: [],
    commands: [],
    views: [{ id: `${id}.home`, title: "Home", component: () => null }],
    opensHome,
  };
}

// The registry is shared across test files and refuses a second registration,
// so these ids are this file's own.
registerModule(fake("opens-home-test", true));
registerModule(fake("asks-first-test", false));

const openTabs = (moduleId: string) =>
  moduleTabs(useWorkbenchTabsStore.getState().modules, moduleId).openTabs;

describe("openHomeWhenAsked", () => {
  beforeEach(() => {
    useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "" });
  });

  test("a module that asks gets its home view when it has nothing open", () => {
    openHomeWhenAsked("opens-home-test");
    expect(openTabs("opens-home-test").map((tab) => tab.viewId)).toEqual(["opens-home-test.home"]);
  });

  test("a module that did not ask keeps the empty state", () => {
    openHomeWhenAsked("asks-first-test");
    expect(openTabs("asks-first-test")).toHaveLength(0);
  });

  test("a module that asks does not get a second home tab when it already has one", () => {
    openHomeWhenAsked("opens-home-test");
    openHomeWhenAsked("opens-home-test");
    expect(openTabs("opens-home-test")).toHaveLength(1);
  });

  test("an unknown module is left alone", () => {
    expect(() => openHomeWhenAsked("nope")).not.toThrow();
  });
});
