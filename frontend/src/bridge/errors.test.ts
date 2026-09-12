import { describe, expect, test } from "bun:test";
import { errorMessage } from "./errors";

/**
 * An error with nothing to say still has to say something.
 *
 * `new Error()` has an empty message, and so does the rejection
 * @wailsio/runtime produces when there is no runtime behind a call. Rendered
 * into an error card that is a card with nothing in it — and a blank
 * explanation is worse than a dull one, because the reader cannot tell a
 * failure from a panel that has not loaded. Found exactly that way: a rail
 * showing an empty box where "could not read the consoles" belonged.
 */
describe("an error with no message", () => {
  test.each([
    ["an Error with an empty message", new Error("")],
    ["an Error with no message at all", new Error()],
    ["an empty string", ""],
    ["null", null],
    ["undefined", undefined],
    ["a bare object", {}],
  ])("%s still explains itself", (_label, value) => {
    expect(errorMessage(value)).toBe("An unexpected error occurred.");
  });

  test("a message that exists is never replaced", () => {
    expect(errorMessage(new Error("relation \"user\" does not exist"))).toBe(
      'relation "user" does not exist',
    );
  });
});
