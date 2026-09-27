import type { TCodeBlockElement } from 'platejs';
import { DOCX_CODE_METRICS } from '@/lib/design-metrics';

import {
  type SlateElementProps,
  type SlateLeafProps,
  SlateElement,
  SlateLeaf,
} from 'platejs/static';
import { getCodeBlockLanguageLabel } from '@/components/editor/plugins/code-block-languages';

type CodeBlockElementStaticProps = SlateElementProps<TCodeBlockElement> & {
  showLanguageLabel?: boolean;
};

export function CodeBlockElementStatic({
  showLanguageLabel = true,
  ...props
}: CodeBlockElementStaticProps) {
  const languageLabel = getCodeBlockLanguageLabel(props.element.lang);

  return (
    <SlateElement
      // Same token classes as CodeBlockElement (code-block-node.tsx), so a
      // static render follows the theme family the editor does.
      className="py-1 **:[.hljs-addition]:bg-(--feedback-success-soft) **:[.hljs-addition]:text-(--status-success) **:[.hljs-deletion]:bg-(--feedback-danger-soft) **:[.hljs-deletion]:text-(--status-danger) **:[.hljs-attr,.hljs-attribute,.hljs-literal,.hljs-meta,.hljs-number,.hljs-operator,.hljs-selector-attr,.hljs-selector-class,.hljs-selector-id,.hljs-variable]:text-(--status-info) **:[.hljs-built\\_in,.hljs-symbol,.hljs-bullet]:text-(--status-warning) **:[.hljs-comment,.hljs-code,.hljs-formula]:text-(--text-tertiary) **:[.hljs-keyword,.hljs-doctag,.hljs-template-tag,.hljs-template-variable,.hljs-type,.hljs-variable.language\\_]:text-(--brand-text) **:[.hljs-name,.hljs-quote,.hljs-selector-tag,.hljs-selector-pseudo]:text-(--status-success) **:[.hljs-regexp,.hljs-string,.hljs-meta_.hljs-string]:text-(--status-success) **:[.hljs-section]:text-(--status-info) **:[.hljs-title,.hljs-title.class\\_,.hljs-title.class\\_.inherited\\_\\_,.hljs-title.function\\_]:text-(--status-recovery) **:[.hljs-emphasis]:italic **:[.hljs-section]:font-bold **:[.hljs-strong]:font-bold"
      {...props}
    >
      <div className="relative rounded-md bg-muted/50">
        {showLanguageLabel && languageLabel && (
          <div
            className="absolute top-1 right-1 z-10 flex h-6 select-none items-center px-2 text-muted-foreground text-caption-11"
            contentEditable={false}
          >
            {languageLabel}
          </div>
        )}

        <pre className="overflow-x-auto p-8 pr-4 text-code-12 tab-size-2 print:break-inside-avoid">
          <code>{props.children}</code>
        </pre>
      </div>
    </SlateElement>
  );
}

export function CodeLineElementStatic(props: SlateElementProps) {
  return <SlateElement {...props} />;
}

export function CodeSyntaxLeafStatic(props: SlateLeafProps) {
  const tokenClassName = props.leaf.className as string;

  return <SlateLeaf className={tokenClassName} {...props} />;
}

/**
 * DOCX-compatible code block components.
 * Uses inline styles for proper rendering in Word documents.
 */

export function CodeBlockElementDocx(
  props: SlateElementProps<TCodeBlockElement>
) {
  return (
    <SlateElement {...props}>
      <div
        style={{
          backgroundColor: '#f5f5f5',
          border: '1px solid #e0e0e0',
          margin: DOCX_CODE_METRICS.margin,
          padding: DOCX_CODE_METRICS.padding,
        }}
      >
        {props.children}
      </div>
    </SlateElement>
  );
}

export function CodeLineElementDocx(props: SlateElementProps) {
  return (
    <SlateElement
      {...props}
      as="p"
      style={{
        fontFamily: "'Courier New', Consolas, monospace",
        fontSize: DOCX_CODE_METRICS.fontSize,
        margin: 0,
        padding: 0,
      }}
    />
  );
}

// Syntax highlighting color map for common token types
const syntaxColors: Record<string, string> = {
  'hljs-addition': '#22863a',
  'hljs-attr': '#005cc5',
  'hljs-attribute': '#005cc5',
  'hljs-built_in': '#e36209',
  'hljs-bullet': '#735c0f',
  'hljs-comment': '#6a737d',
  'hljs-deletion': '#b31d28',
  'hljs-doctag': '#d73a49',
  'hljs-emphasis': '#24292e',
  'hljs-formula': '#6a737d',
  'hljs-keyword': '#d73a49',
  'hljs-literal': '#005cc5',
  'hljs-meta': '#005cc5',
  'hljs-name': '#22863a',
  'hljs-number': '#005cc5',
  'hljs-operator': '#005cc5',
  'hljs-quote': '#22863a',
  'hljs-regexp': '#032f62',
  'hljs-section': '#005cc5',
  'hljs-selector-attr': '#005cc5',
  'hljs-selector-class': '#005cc5',
  'hljs-selector-id': '#005cc5',
  'hljs-selector-pseudo': '#22863a',
  'hljs-selector-tag': '#22863a',
  'hljs-string': '#032f62',
  'hljs-strong': '#24292e',
  'hljs-symbol': '#e36209',
  'hljs-template-tag': '#d73a49',
  'hljs-template-variable': '#d73a49',
  'hljs-title': '#6f42c1',
  'hljs-type': '#d73a49',
  'hljs-variable': '#005cc5',
};

// Convert regular spaces to non-breaking spaces to preserve indentation in Word
const preserveSpaces = (text: string): string => {
  // Replace regular spaces with non-breaking spaces
  return text.replace(/ /g, '\u00A0');
};

export function CodeSyntaxLeafDocx(props: SlateLeafProps) {
  const tokenClassName = props.leaf.className as string;

  // Extract color from className
  let color: string | undefined;
  let fontWeight: string | undefined;
  let fontStyle: string | undefined;

  if (tokenClassName) {
    const classes = tokenClassName.split(' ');
    for (const cls of classes) {
      if (syntaxColors[cls]) {
        color = syntaxColors[cls];
      }
      if (cls === 'hljs-strong' || cls === 'hljs-section') {
        fontWeight = 'bold';
      }
      if (cls === 'hljs-emphasis') {
        fontStyle = 'italic';
      }
    }
  }

  // Get the text content and preserve spaces
  const text = props.leaf.text as string;
  const preservedText = preserveSpaces(text);

  return (
    <span
      data-slate-leaf="true"
      style={{
        color,
        fontFamily: "'Courier New', Consolas, monospace",
        fontSize: DOCX_CODE_METRICS.fontSize,
        fontStyle,
        fontWeight,
      }}
    >
      {preservedText}
    </span>
  );
}
