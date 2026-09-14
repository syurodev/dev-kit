import type { SlateLeafProps } from 'platejs/static';

import { SlateLeaf } from 'platejs/static';

export function CodeLeafStatic(props: SlateLeafProps) {
  return (
    <SlateLeaf
      {...props}
      as="code"
      className="whitespace-pre-wrap rounded-md bg-muted px-(--space-inline-code-x) py-(--space-inline-code-y) text-code-12"
    >
      {props.children}
    </SlateLeaf>
  );
}
