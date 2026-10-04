import { beforeEach, expect, test } from "bun:test";
import { useSSHWorkspaceState, runSSHAction } from "./workspaceState";
import { validateConnection, validateForward } from "./validation";

beforeEach(() => useSSHWorkspaceState.setState({ entries: {} }));
test("running work survives navigation and rejects double submission", async () => {
  let finish!: () => void;
  const deferred = new Promise<void>(resolve => { finish = resolve; });
  const running = runSSHAction("a", "Running command…", "commands", async () => {
    await deferred;
    useSSHWorkspaceState.getState().patch("a", { output: "result" });
  });
  expect(await runSSHAction("a", "Duplicate", "commands", async () => { throw new Error("must not run"); })).toBe("busy");
  useSSHWorkspaceState.getState().patch("b", { command: "other command", page: "tunnels" });
  finish();
  expect(await running).toBe("success");
  expect(useSSHWorkspaceState.getState().entries.a.output).toBe("result");
  expect(useSSHWorkspaceState.getState().entries.a.busy).toBe("");
  expect(useSSHWorkspaceState.getState().entries.b.command).toBe("other command");
});
test("a failed action keeps the draft and a scoped error for retry", async () => {
  useSSHWorkspaceState.getState().patch("a", { command: "uptime" });
  expect(await runSSHAction("a", "Running", "commands", async () => { throw new Error("Connection lost"); })).toBe("error");
  const state = useSSHWorkspaceState.getState().entries.a;
  expect(state.command).toBe("uptime");
  expect(state.error).toBe("Connection lost");
  expect(state.scope).toBe("commands");
  expect(state.busy).toBe("");
  await runSSHAction("a", "Retry", "commands", async () => {});
  expect(useSSHWorkspaceState.getState().entries.a.error).toBe("");
});
test("editing/deleting clears only the affected connection's session state", () => {
  const store = useSSHWorkspaceState.getState();
  store.patch("a", { output: "old result" }); store.patch("b", { command: "keep" });
  store.forget("a");
  expect(useSSHWorkspaceState.getState().entries.a).toBeUndefined();
  expect(useSSHWorkspaceState.getState().entries.b.command).toBe("keep");
});
test("blank, fractional and out-of-range ports are field errors", () => {
  const form = { name: "server", host: "localhost", port: "", username: "me", env: "dev", secret: "" };
  for (const port of ["", "0", "1.5", "65536"]) expect(validateConnection({ ...form, port }, true).port).toBeTruthy();
  expect(validateConnection({ ...form, port: "22" }, true)).toEqual({});
  expect(validateConnection({ ...form, port: "22" }, false).secret).toBeTruthy();
  expect(validateConnection({ ...form, name: " ", port: "22" }, true).name).toBeTruthy();
  expect(validateForward({ listenPort: "", targetPort: "", targetHost: " " })).toHaveProperty("listenPort");
  expect(validateForward({ listenPort: 0, targetPort: 5432, targetHost: "localhost" })).toEqual({});
});
