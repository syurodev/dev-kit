import type { SlateLeafProps } from 'platejs/static';

import { SlateLeaf } from 'platejs/static';

export function KbdLeafStatic(props: SlateLeafProps) {
  return (
    <SlateLeaf
      {...props}
      as="kbd"
      className="rounded border border-border bg-muted px-1.5 py-0.5 text-code-12 shadow-kbd dark:shadow-kbd-dark"
    >
      {props.children}
    </SlateLeaf>
  );
}
