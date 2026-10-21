# Agent Hub rail — a branch with many child sessions stays short

Status: approved in conversation 2026-10-21. Follows the rail work in
`2026-10-13-agent-hub-wide-layout-design.md` (rows, tree) and the branch rules in
`AGENTS.md` ("The rail is collapsible groups of cards…").

## Problem

A parent that started many sessions draws every one of them. A child is a 50px
card, and the branch is either open (all of them) or folded (none). One
orchestrating session with 11 finished children fills the whole rail: the groups
and sessions below it are pushed off screen, though nine of those children are
done and nobody is looking at them.

## Goals

1. A branch's height follows what is going on, not how many sessions it ever
   started.
2. Nothing is lost: hidden children are one press away, and what needs the
   person is still marked.
3. A branch with few children looks exactly as it does today.

Non-goals: compact rows for finished children, a different card, remembering the
"show all" choice across restarts, changing Needs you or the group's own
"5 most recent trees" cut.

## Rule

For every node with children, at any level:

1. `R` = the number of its **direct** children whose `status` is `running`.
2. `limit = max(MIN_VISIBLE_CHILDREN, R)`, with `MIN_VISIBLE_CHILDREN = 3`.
3. The children drawn are chosen in this order until `limit` is reached:
   1. every running child;
   2. a child that is, or holds in its subtree, the session on screen or a
      pinned one (`heldBranches` already keeps such a branch open; it must stay
      drawn too). These are exceptions: they are drawn even when that goes past
      `limit`;
   3. the remaining children, newest first (`compareRecent`).
4. The drawn children keep the order they have today (newest first). Hiding only
   filters.
5. Children not drawn fold into one row at the end of the list:
   `+ N more · Show all`. Pressing it draws every child; the row then reads
   `Show fewer` and pressing it goes back.
6. A node with `limit` children or fewer draws no such row: nothing changes for it.

Only `status === "running"` counts as active. A waiting or failed child is drawn
in Needs you, not in the tree (`ask={null}` in `SessionBranch`), and stays so.

### What stays true for hidden children

- `branchSignals` and `branchHint` read the whole list, so a hidden child still
  marks its parent (waiting / failed always; running / unread when folded).
- The parent's count (`childCount` = `node.descendants`) stays the real total.
- The "more" row carries the unread mark (the card's message icon and a count)
  for folded sessions, **including their descendants** (they are folded too),
  that hold a peer message the person has not seen. Waiting/failed ones are
  marked on the parent already.
- A folded **direct** child cannot be running (rule 3.1), but a folded idle
  child can hold a running descendant, and the parent's own dot speaks for
  running work only while the branch is folded. So the row also says
  `{n} running` (a dot and the count, as visible text) for running sessions
  among the folded ones, descendants included. The limit still counts only
  direct running children; this is a mark, not a rule change.
- Folding the branch (the existing chevron) hides the whole list including the
  "more" row, as today; "Show all" is separate from fold and is forgotten when
  the branch is folded and opened again.

## Design

### `sidebar/tree.ts` (pure, tested)

- `MIN_VISIBLE_CHILDREN = 3`.
- `visibleChildren(children, { activeId, pinned })` returns
  `{ shown: SessionNode[]; hidden: SessionNode[] }`.
  - `shown` keeps the input order; `hidden` is the rest, also in input order.
  - A child "holds" a session when it is that session or any node under it is
    (use `flattenNode`).
  - A child is running when `child.session.status === "running"`.
  - Pure and level-agnostic, so nested nodes use it unchanged.

### `sidebar/SessionBranch.tsx`

- Call `visibleChildren` when the branch is open; `ctx` already carries
  `activeId` and `pinned`.
- Local state `all: boolean` (component state, not stored); `all` or an empty
  `hidden` draws every child.
- The list id (`listId`) is the `aria-controls` target of the "more" row.
- `Guides` takes `last` from the position among the **drawn** rows plus the
  "more" row, so the vertical line ends at the row that is really last.
- The "more" row sits in the same indented list as its siblings, with its own
  elbow so it reads as part of the branch.

### `BranchMore` (new, same folder)

- A `button`, about 28px high, `aria-expanded`, `aria-controls`, text
  `+{n} more · Show all` / `Show fewer`, `{n} running` when folded sessions
  run, and the unread mark (message icon and count) when folded sessions hold a
  peer message. The button has no `aria-label`: its visible text is its name.
- Tailwind classes and the theme's chrome tokens; the only inline style is the
  geometry the guides already use. No new fill, border or shadow of its own.
- Keyboard: a normal button (Enter / Space). It is a `listitem` of the branch
  list, so a screen reader hears it as the last item.

## Testing

- `tree.test.ts`:
  - 11 children, none running → 3 shown, 8 hidden, newest three;
  - 5 running + 6 idle → the 5 running, 6 hidden;
  - 1 running + 10 idle → 3 shown (the running one + two newest idle);
  - 2 running + 3 idle → 3 shown;
  - the session on screen / a pinned one outside the newest three is shown
    (also when it is a grandchild, so its ancestor child is shown), going past
    `limit`;
  - ≤ 3 children → nothing hidden; order of `shown` is the input's.
- `AgentSessionSidebar.test.tsx`: a branch with 11 idle children renders 3 cards
  and a "+ 8 more" button; pressing it renders 11 and "Show fewer"; the parent
  keeps its count of 11; a hidden child with a peer message puts the dot on the
  row; a branch with 3 children has no row.
- Gallery `?gallery#agenthub-session-rail`: add a "many-children" rail (11 idle
  children, and a variant with 4 running) for a visual check. Not seen in the
  native app unless the implementer runs it.

## Open choices (decided)

- Minimum 3, not 5: the group's cut is for trees; a branch is read as a list of
  recent work.
- No persistence of "Show all": YAGNI; add to `agentPins` if people ask.
- Compact rows for finished children: left out; the "more" row already removes
  most of the height (11 × 50px → 3 × 50px + 28px).
