import remarkGfm from "remark-gfm";
import { MarkdownPlugin } from "@platejs/markdown";
import { SlashInputPlugin, SlashPlugin } from "@platejs/slash-command/react";
import { NoteLinkElement, NoteLinkPlugin } from "./NoteLink";
import { TogglePlugin } from "@platejs/toggle/react";
import { KEYS, NodeApi, PathApi, PointApi, type SlateEditor } from "platejs";
import { createPlatePlugin } from "platejs/react";

import { BasicBlocksKit } from "@/components/editor/plugins/basic-blocks-kit";
import { BasicMarksKit } from "@/components/editor/plugins/basic-marks-kit";
import { CodeBlockKit } from "@/components/editor/plugins/code-block-kit";
import { LinkKit } from "@/components/editor/plugins/link-kit";
import { ListKit } from "@/components/editor/plugins/list-kit";
import { TableKit } from "@/components/editor/plugins/table-kit";

import { ImageElement } from "@/components/ui/media-image-node";

import { CalloutElement } from "./callout";
import { notesMarkdownRules } from "./markdownRules";
import { RenderedCodeBlock } from "./RenderedCodeBlock";
import { TocElement } from "./toc";
import { ImageUploadPlugin } from "./ImageUpload";
import { SlashInputElement } from "./SlashInput";

/**
 * CodeBlockKit's own code_block plugin renders every language with plain
 * syntax highlighting. Swap in RenderedCodeBlock, which draws `mermaid` and
 * `math` as what they describe and leaves every other language as CodeBlockKit
 * had it.
 *
 * `configure`, not `withComponent`: the kit sets its component with
 * `configure({ node: { component } })`, and Plate applies configurations after
 * extensions, so a `withComponent` here lost to it and the swap never
 * happened — every ```mermaid block showed as source.
 */
const codeBlockKitWithPreviews = CodeBlockKit.map((plugin) =>
  plugin.key === "code_block" ? plugin.configure({ node: { component: RenderedCodeBlock } }) : plugin,
);

/**
 * A callout is a box of blocks. Enter on its empty last line leaves it, and
 * Backspace at its very start unwraps it — the way a quote or a list ends.
 *
 * Done here rather than with `rules.break` / `rules.delete`: those are read
 * from the plugin of the block the caret is in, which is the paragraph inside
 * the callout, never the callout.
 */
const CalloutPlugin = createPlatePlugin({
  key: KEYS.callout,
  node: { isElement: true, component: CalloutElement },
}).overrideEditor(({ editor, tf: { insertBreak, deleteBackward } }) => ({
  transforms: {
    insertBreak() {
      if (!leaveCallout(editor)) insertBreak();
    },
    deleteBackward(unit) {
      if (!unwrapCalloutAtStart(editor)) deleteBackward(unit);
    },
  },
}));

/** The caret is on the empty last paragraph of a callout that has others: move it out, below. */
function leaveCallout(editor: SlateEditor): boolean {
  if (!editor.selection || editor.api.isExpanded()) return false;
  const callout = editor.api.above({ match: { type: KEYS.callout } });
  const block = editor.api.block();
  if (!callout || !block) return false;
  const [calloutNode, calloutPath] = callout;
  const [blockNode, blockPath] = block;
  const isLast = blockPath[blockPath.length - 1] === calloutNode.children.length - 1;
  if (!PathApi.equals(PathApi.parent(blockPath), calloutPath) || !isLast || calloutNode.children.length < 2) {
    return false;
  }
  // An empty list item ends its list first; that is the list's to do.
  if (blockNode.type !== KEYS.p || NodeApi.string(blockNode) !== "" || blockNode.listStyleType) return false;
  editor.tf.moveNodes({ at: blockPath, to: PathApi.next(calloutPath) });
  return true;
}

/** The caret is at the very start of a callout's first block: lift the blocks out of it. */
function unwrapCalloutAtStart(editor: SlateEditor): boolean {
  const { selection } = editor;
  if (!selection || editor.api.isExpanded()) return false;
  const callout = editor.api.above({ match: { type: KEYS.callout } });
  if (!callout) return false;
  const [calloutNode, calloutPath] = callout;
  const first = calloutNode.children[0] as { listStyleType?: string } | undefined;
  if (first?.listStyleType || !PointApi.equals(selection.anchor, editor.api.start(calloutPath)!)) return false;
  editor.tf.unwrapNodes({ at: calloutPath });
  return true;
}

/** A table of contents is a void block: it holds no text, it lists the headings. */
const TocPlugin = createPlatePlugin({
  key: KEYS.toc,
  node: { isElement: true, isVoid: true, component: TocElement },
});

export const NotesEditorKit = [
  ...BasicBlocksKit,
  ...BasicMarksKit,
  ...ListKit,
  ...LinkKit,
  ...TableKit,
  ...codeBlockKitWithPreviews,
  CalloutPlugin,
  TocPlugin,
  TogglePlugin,
  ImageUploadPlugin.withComponent(ImageElement),
  SlashPlugin.configure({
    options: {
      triggerQuery: (editor: SlateEditor) =>
        !editor.api.some({
          match: { type: editor.getType(KEYS.codeBlock) },
        }),
    },
  }),
  SlashInputPlugin.withComponent(SlashInputElement),
  NoteLinkPlugin.withComponent(NoteLinkElement),
  MarkdownPlugin.configure({
    options: {
      remarkPlugins: [remarkGfm],
      rules: notesMarkdownRules,
    },
  }),
];
