import { expect, test } from "bun:test";
import type { ReactElement } from "react";
import { withGitFolder, type GitViewProps } from "./GitFolderGate";

test("the repository wrapper forwards document params to the gate", () => {
  const params = { rev: "a1234567", staged: true };
  const View = (_props: GitViewProps) => null;
  const Wrapped = withGitFolder(View, { bar: false }) as (
    props: GitViewProps,
  ) => ReactElement<{ params?: Record<string, unknown>; View: unknown }>;
  const element = Wrapped({ params });
  expect(element.props.params).toBe(params);
  expect(element.props.View).toBe(View);
});
