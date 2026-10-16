import { YamlEditor } from "@/components/yaml/YamlEditor";
import { useRef, useState } from "react";
import type { EditorView } from "@codemirror/view";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { EditingControls } from "@/components/code-editor/EditingControls";
import { StructureControls } from "@/components/code-editor/StructureControls";
import { useLanguageExtension } from "@/components/code/useLanguageExtension";
import { FileIcon } from "@/components/file-icon/FileIcon";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

const YAML = `# Liquibase migration — YAML structure + embedded SQL

databaseChangeLog:
  - changeSet:
      id: create-billing-procedure
      author: migrations
      preConditions:
        onFail: MARK_RAN
        sqlCheck:
          expectedResult: 0
          sql: |
            SELECT COUNT(*) FROM INFORMATION_SCHEMA.ROUTINES
            WHERE ROUTINE_SCHEMA = DATABASE()
              AND ROUTINE_NAME = 'billing_statements'
              AND ROUTINE_TYPE = 'PROCEDURE';
      changes:
        - sql:
            splitStatements: false
            sql: |
              CREATE PROCEDURE billing_statements(IN customerId BIGINT, IN fromDate VARCHAR(50), IN toDate VARCHAR(50), OUT message VARCHAR(255))
              READS SQL DATA
              BEGIN
                -- Keep the SQL readable inside its YAML block
                DECLARE statement_count INT DEFAULT 0;
                SELECT COUNT(*) INTO statement_count
                  FROM billing_statements
                  WHERE customer_id = customerId;
                SET message = 'Success';
              END;
      description: >-
        This is ordinary YAML text, not SQL.
        SELECT here remains plain text.
`;
const MAKE = `# GNU Make — targets, variables and shell recipes
CC := gcc
CFLAGS ?= -Wall -O2
SOURCES = $(wildcard src/*.c)
OBJECTS = $(patsubst %.c,%.o,$(SOURCES))

include config.mk
.PHONY: all clean test
all: build/app

build/app: $(OBJECTS)
\t@echo "Linking $@"
\t$(CC) $(CFLAGS) -o $@ $^

%.o: %.c
\t$(CC) $(CFLAGS) -c $< -o $@

ifeq ($(DEBUG),1)
CFLAGS += -g
endif

clean:
\trm -f $(OBJECTS) build/app
`;

/** A bridge-free version of the file editor, including its real YAML controls. */
export function FileSyntaxSection() {
  const [language, setLanguage] = useState("yaml");
  const [sources, setSources] = useState({ yaml: YAML, makefile: MAKE });
  const [wrapped, setWrapped] = useState(true);
  const [width, setWidth] = useState(1000);
  const view = useRef<EditorView | null>(null);
  const support = useLanguageExtension(language);

  const name = language === "yaml" ? "billing-statements.yaml" : "Makefile";
  return <section id="file-syntax" className="flex flex-col gap-3 px-6 py-5">
    <h2 className="text-title-16 text-text-primary">File syntax · YAML and Makefile</h2>
    <div className="flex flex-wrap items-center gap-2">
      <Tabs value={language} onValueChange={setLanguage}><TabsList><TabsTrigger value="yaml">YAML</TabsTrigger><TabsTrigger value="makefile">Makefile</TabsTrigger></TabsList></Tabs>
      {[320, 640, 1000].map((size) => <Button key={size} size="xs" variant={size === width ? "secondary" : "ghost"} onClick={() => setWidth(size)}>{size}px</Button>)}
    </div>
    <div className="max-w-full overflow-hidden rounded-control border border-border-subtle bg-surface-base" style={{ width }} data-testid="file-syntax-pane">
      <div className="flex min-w-0 items-center gap-2 border-b border-border-subtle px-2 py-1">
        <FileIcon path={name} className="size-3.5" /><span className="min-w-0 flex-1 truncate text-code-12 text-text-secondary">{name}</span>
        {language !== "yaml" ? <><EditingControls getView={() => view.current} indentStyle="tabs" /><StructureControls wrapped={wrapped} onWrapChange={setWrapped} getView={() => view.current} /></> : null}
      </div>
      <div className="h-160" data-testid="file-syntax-editor">
        {language === "yaml" ? <YamlEditor file="billing-statements.yaml" documentKey="gallery:yaml" value={sources.yaml} onChange={(yaml) => setSources((previous) => ({ ...previous, yaml }))} extensions={support} onView={(next) => { view.current = next; }} /> :
          <CodeEditor indentStyle="tabs" value={sources.makefile} onChange={(makefile) => setSources((previous) => ({ ...previous, makefile }))} extensions={support} wrap={wrapped} onWrapChange={setWrapped} onView={(next) => { view.current = next; }} />}

      </div>
    </div>
  </section>;
}
