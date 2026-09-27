'use client';

import * as React from 'react';

import { formatCodeBlock, isLangSupported } from '@platejs/code-block';
import { BracesIcon, Check, CheckIcon, CopyIcon } from 'lucide-react';
import { type TCodeBlockElement, type TCodeSyntaxLeaf, NodeApi } from 'platejs';
import {
  type PlateElementProps,
  type PlateLeafProps,
  PlateElement,
  PlateLeaf,
} from 'platejs/react';
import { useEditorRef, useElement, useReadOnly } from 'platejs/react';

import { Button } from '@/components/ui/button';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import { cn } from '@/lib/utils';
import {
  CODE_BLOCK_LANGUAGES,
  getCodeBlockLanguageLabel,
} from '@/components/editor/plugins/code-block-languages';

type CodeBlockElementProps = PlateElementProps<TCodeBlockElement> & {
  showLanguageLabel?: boolean;
};

export function CodeBlockElement({
  showLanguageLabel = true,
  ...props
}: CodeBlockElementProps) {
  const { editor, element } = props;

  return (
    <PlateElement
      // Tokens, not the two GitHub palettes this used to carry as twenty-five
      // literal hexes behind a `dark:` prefix. Those followed the MODE and
      // nothing else, so a Dracula app rendered GitHub-coloured code blocks —
      // and the light half never matched any theme but GitHub Light. The
      // colours are the --syntax-* tokens every editor uses (devkitSyntax.ts):
      // the status ramp by default, a family's own editor palette where it
      // has one, and themeContrast.test.ts scores both.
      className="py-1 **:[.hljs-addition]:bg-(--feedback-success-soft) **:[.hljs-addition]:text-(--status-success) **:[.hljs-deletion]:bg-(--feedback-danger-soft) **:[.hljs-deletion]:text-(--status-danger) **:[.hljs-keyword,.hljs-doctag,.hljs-template-tag,.hljs-selector-tag,.hljs-variable.language\\_]:text-(--syntax-keyword) **:[.hljs-string,.hljs-regexp,.hljs-meta_.hljs-string]:text-(--syntax-string) **:[.hljs-number,.hljs-literal,.hljs-symbol,.hljs-bullet]:text-(--syntax-number) **:[.hljs-type,.hljs-built\\_in,.hljs-name,.hljs-title,.hljs-title.class\\_,.hljs-title.class\\_.inherited\\_\\_]:text-(--syntax-type) **:[.hljs-title.function\\_]:text-(--syntax-function) **:[.hljs-attr,.hljs-attribute,.hljs-property,.hljs-variable,.hljs-template-variable,.hljs-selector-attr,.hljs-selector-class,.hljs-selector-id,.hljs-selector-pseudo]:text-(--syntax-definition) **:[.hljs-comment,.hljs-code,.hljs-formula,.hljs-quote]:text-(--syntax-comment) **:[.hljs-meta,.hljs-meta_.hljs-keyword]:text-(--syntax-meta) **:[.hljs-section]:text-(--text-primary) **:[.hljs-section]:font-bold **:[.hljs-emphasis]:italic **:[.hljs-strong]:font-bold"
      {...props}
    >
      <div className="relative rounded-md bg-muted/50">
        <pre className="overflow-x-auto p-8 pr-4 text-code-12 tab-size-2 print:break-inside-avoid">
          <code>{props.children}</code>
        </pre>

        <div
          className="absolute top-1 right-1 z-10 flex select-none gap-0.5"
          contentEditable={false}
        >
          {isLangSupported(element.lang) && (
            <Button
              size="icon"
              variant="ghost"
              className="size-6 text-label-12"
              onClick={() => formatCodeBlock(editor, { element })}
              title="Format code"
            >
              <BracesIcon className="!size-3.5 text-muted-foreground" />
            </Button>
          )}

          <CodeBlockCombobox showLanguageLabel={showLanguageLabel} />

          <CopyButton
            size="icon"
            variant="ghost"
            className="size-6 gap-1 text-muted-foreground text-label-12"
            value={() => NodeApi.string(element)}
          />
        </div>
      </div>
    </PlateElement>
  );
}

function CodeBlockCombobox({
  showLanguageLabel,
}: {
  showLanguageLabel: boolean;
}) {
  const [open, setOpen] = React.useState(false);
  const readOnly = useReadOnly();
  const editor = useEditorRef();
  const element = useElement<TCodeBlockElement>();
  const value = element.lang || 'plaintext';
  const [searchValue, setSearchValue] = React.useState('');

  const items = React.useMemo(
    () =>
      CODE_BLOCK_LANGUAGES.filter(
        (language) =>
          !searchValue ||
          language.label.toLowerCase().includes(searchValue.toLowerCase())
      ),
    [searchValue]
  );

  if (readOnly) {
    if (!showLanguageLabel) return null;

    return <CodeBlockLanguageLabel lang={element.lang} />;
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          size="sm"
          variant="ghost"
          className="h-6 select-none justify-between gap-1 px-2 text-muted-foreground text-caption-11"
          aria-expanded={open}
          role="combobox"
        >
          {getCodeBlockLanguageLabel(value) ?? 'Plain Text'}
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className="w-50 p-0"
        onCloseAutoFocus={() => setSearchValue('')}
      >
        <Command shouldFilter={false}>
          <CommandInput
            className="h-9"
            value={searchValue}
            onValueChange={(value) => setSearchValue(value)}
            placeholder="Search language..."
          />
          <CommandEmpty>No language found.</CommandEmpty>

          <CommandList className="h-86 overflow-y-auto">
            <CommandGroup>
              {items.map((language) => (
                <CommandItem
                  key={language.label}
                  className="cursor-pointer"
                  value={language.value}
                  onSelect={(value) => {
                    editor.tf.setNodes<TCodeBlockElement>(
                      { lang: value },
                      { at: element }
                    );
                    setSearchValue(value);
                    setOpen(false);
                  }}
                >
                  <Check
                    className={cn(
                      value === language.value ? 'opacity-100' : 'opacity-0'
                    )}
                  />
                  {language.label}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function CodeBlockLanguageLabel({ lang }: { lang?: string | null }) {
  const label = getCodeBlockLanguageLabel(lang);

  if (!label) return null;

  return (
    <span className="flex h-6 select-none items-center px-2 text-muted-foreground text-caption-11">
      {label}
    </span>
  );
}

function CopyButton({
  value,
  ...props
}: { value: (() => string) | string } & Omit<
  React.ComponentProps<typeof Button>,
  'value'
>) {
  const [hasCopied, setHasCopied] = React.useState(false);

  React.useEffect(() => {
    setTimeout(() => {
      setHasCopied(false);
    }, 2000);
  }, [hasCopied]);

  return (
    <Button
      onClick={() => {
        void navigator.clipboard.writeText(
          typeof value === 'function' ? value() : value
        );
        setHasCopied(true);
      }}
      {...props}
    >
      <span className="sr-only">Copy</span>
      {hasCopied ? (
        <CheckIcon className="!size-3" />
      ) : (
        <CopyIcon className="!size-3" />
      )}
    </Button>
  );
}

export function CodeLineElement(props: PlateElementProps) {
  return <PlateElement {...props} />;
}

export function CodeSyntaxLeaf(props: PlateLeafProps<TCodeSyntaxLeaf>) {
  const tokenClassName = props.leaf.className as string;

  return <PlateLeaf className={tokenClassName} {...props} />;
}
