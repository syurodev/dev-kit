import { expect, test } from "bun:test";
import { createGitUI, filterCommits } from "./gitUI";

test("Git serializes writes before invoking a second action, including another repository", async () => {
  const ui = createGitUI();
  let finish!: () => void;
  let calls = 0;
  const first = ui.run(
    "/one",
    "Committing",
    () =>
      new Promise<void>((resolve) => {
        calls++;
        finish = resolve;
      }),
    "Committed",
  );
  expect(ui.useStore.getState().operations["/one"].label).toBe("Committing");
  expect(
    await ui.run("/one", "Committing", async () => {
      calls++;
    }),
  ).toBe(false);
  expect(
    await ui.run("/two", "Staging", async () => {
      calls++;
    }),
  ).toBe(false);
  expect(calls).toBe(1);
  finish();
  expect(await first).toBe(true);
  expect(ui.useStore.getState().operations["/one"]).toEqual({
    label: "",
    error: "",
    notice: "Committed",
  });
  expect(
    await ui.run("/two", "Staging", async () => {
      calls++;
    }),
  ).toBe(true);
});

test("failed writes retain drafts and error scope; successful retry can clear only its message", async () => {
  const ui = createGitUI();
  ui.setDraft("/one", { message: "my commit", historySearch: "Alice" });
  ui.setDraft("/two", { message: "another repo" });
  expect(
    await ui.run("/one", "Commit", async () => {
      throw new Error("hook refused");
    }),
  ).toBe(false);
  expect(ui.useStore.getState().drafts["/one"].message).toBe("my commit");
  expect(ui.useStore.getState().operations["/one"].error).toBe("hook refused");
  expect(ui.useStore.getState().operations["/two"]).toBeUndefined();
  expect(
    await ui.run(
      "/one",
      "Commit",
      async () => {
        ui.setDraft("/one", { message: "" });
      },
      "Committed",
    ),
  ).toBe(true);
  expect(ui.useStore.getState().drafts["/one"].historySearch).toBe("Alice");
  expect(ui.useStore.getState().drafts["/two"].message).toBe("another repo");
  expect(ui.useStore.getState().operations["/one"].error).toBe("");
});

test("history search matches all query terms across SHA, author, subject and body", () => {
  const commits = [
    {
      sha: "abcd1234",
      subject: "Fix diff",
      author: "Alice",
      body: "Task: DK-12",
    },
    { sha: "ff1234", subject: "Update", author: "Bob", body: "" },
  ];
  expect(filterCommits(commits, " ALICE dk-12 ")).toEqual([commits[0]]);
  expect(filterCommits(commits, "abcd FIX")).toEqual([commits[0]]);
  expect(filterCommits(commits, "alice update")).toEqual([]);
  expect(filterCommits(commits, " ")).toEqual(commits);
});

test("clearing prior feedback cannot release a pending operation", async () => {
  const ui = createGitUI();
  let finish!: () => void;
  const pending = ui.run(
    "/one",
    "Stash",
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    "Stashed",
  );
  ui.clearFeedback("/one");
  expect(ui.useStore.getState().operations["/one"].label).toBe("Stash");
  finish();
  await pending;
  ui.clearFeedback("/one");
  expect(ui.useStore.getState().operations["/one"]).toEqual({
    label: "",
    error: "",
    notice: "",
  });
});
