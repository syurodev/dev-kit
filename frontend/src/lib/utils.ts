import { clsx, type ClassValue } from "clsx"
import { extendTailwindMerge } from "tailwind-merge"

/**
 * The 18 type utilities declared in index.css. tailwind-merge has to be told
 * they are font sizes.
 *
 * Without this it reads an unrecognised `text-*` as a colour, treats
 * `text-body-13-medium` and `text-action-primary-foreground` as the same kind of
 * class, and drops whichever came first. Measured: the primary button rendered
 * near-white on lime (~1.3:1, illegible) with its foreground class stripped, and
 * nothing failed — `renderToString` + `toContain` cannot see a merged-away
 * class.
 */
const TYPE_UTILITIES = [
  "display-32",
  "heading-24",
  "title-16",
  "title-14",
  "title-13",
  "title-11",
  "body-16",
  "body-14-medium",
  "body-13",
  "body-13-medium",
  "body-12",
  "label-12",
  "label-11-caps",
  "caption-11",
  "caption-11-medium",
  "code-12",
  "code-12-medium",
  "code-11",
]

const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      "font-size": [{ text: TYPE_UTILITIES }],
    },
  },
})

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
