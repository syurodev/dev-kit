import { describe, expect, test } from "bun:test";
import { createConsoleFiles, type ConsoleFile } from "./consoleFiles";

const file = (id: string, sql: string): ConsoleFile => ({
  id,
  profileId: "p1",
  database: "public",
  sql,
});

function recorder() {
  const saved: ConsoleFile[] = [];
  const removed: string[] = [];
  return {
    saved,
    removed,
    io: {
      save: async (console: ConsoleFile) => {
        saved.push(console);
      },
      remove: async (id: string) => {
        removed.push(id);
      },
    },
  };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 5));

describe("when a console reaches the disk", () => {
  test("a burst of typing is one write, with the last text", async () => {
    const { saved, io } = recorder();
    const files = createConsoleFiles(io, 1);

    files.schedule(file("c1", "SEL"));
    files.schedule(file("c1", "SELEC"));
    files.schedule(file("c1", "SELECT 1"));
    await settle();

    expect(saved).toHaveLength(1);
    expect(saved[0]?.sql).toBe("SELECT 1");
  });

  test("two consoles are two files", async () => {
    const { saved, io } = recorder();
    const files = createConsoleFiles(io, 1);

    files.schedule(file("c1", "SELECT 1"));
    files.schedule(file("c2", "SELECT 2"));
    await settle();

    expect(saved.map((c) => c.id).sort()).toEqual(["c1", "c2"]);
  });

  /**
   * The bug this exists to prevent: closing a tab deletes the file, and the
   * debounce that was already in flight writes it straight back. A console
   * you closed comes back after the next relaunch.
   */
  test("closing cancels the write that was already queued", async () => {
    const { saved, removed, io } = recorder();
    const files = createConsoleFiles(io, 1);

    files.schedule(file("c1", "SELECT 1"));
    files.forget("c1");
    await settle();

    expect(removed).toEqual(["c1"]);
    expect(saved, "a closed console wrote itself back").toHaveLength(0);
  });

  test("a disk that refuses does not reject into the window", async () => {
    const files = createConsoleFiles(
      {
        save: () => Promise.reject(new Error("read-only file system")),
        remove: () => Promise.reject(new Error("read-only file system")),
      },
      1,
    );

    files.schedule(file("c1", "SELECT 1"));
    files.forget("c2");
    await settle();
    // Reaching here without an unhandled rejection is the assertion; the
    // console keeps working with nowhere to be written.
    expect(true).toBe(true);
  });

  test("flush writes what is queued without waiting the window out", () => {
    const { saved, io } = recorder();
    const files = createConsoleFiles(io, 10_000);

    files.schedule(file("c1", "SELECT 1"));
    files.flush();

    expect(saved).toHaveLength(1);
  });
});
