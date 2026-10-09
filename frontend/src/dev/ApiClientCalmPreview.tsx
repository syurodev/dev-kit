import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { KeyValueTable } from "@/modules/apiclient/KeyValueTable";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { VariablesForm } from "@/modules/apiclient/VariablesForm";
import type { VarRow } from "@/modules/apiclient/variablesModel";
import { RequestToolbar } from "@/modules/apiclient/RequestToolbar";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { ResponsePane } from "@/modules/apiclient/ResponsePane";
import { ImportDialogView } from "@/modules/apiclient/ImportDialogView";
import type { ImportKind } from "@/modules/apiclient/Dialogs";
import type { KV } from "@/bridge/apiclient";

/** UI fixtures only. No request is sent and no history is read. */
export function ApiClientCalmPreview() {
  const [variablesOpen, setVariablesOpen] = useState(false);
  const [variables, setVariables] = useState<VarRow[]>([
    { key: "baseUrl", value: "https://api.example.com", secret: false },
    { key: "apiKey", value: "", secret: true, existing: true },
  ]);
  const [envName, setEnvName] = useState("API staging");
  const [envTag, setEnvTag] = useState("staging");
  const [name, setName] = useState("List users");
  const [url, setUrl] = useState("{{baseUrl}}/v1/users");
  const [method, setMethod] = useState("GET");
  const [environment, setEnvironment] = useState("staging");
  const [folder, setFolder] = useState("users");
  const [dirty, setDirty] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [kind, setKind] = useState<ImportKind>("file");
  const [texts, setTexts] = useState<Record<ImportKind, string>>({ curl: "", file: "" });
  const text = texts[kind];
  const setText = (value: string) => setTexts((current) => ({ ...current, [kind]: value }));
  const [fileName, setFileName] = useState("");
  const [format, setFormat] = useState("");
  const [tag, setTag] = useState("");
  const [error, setError] = useState("");
  const [rows, setRows] = useState<KV[]>([
    { key: "limit", value: "20" },
    { key: "sort", value: "created_at", disabled: true },
  ]);
  const [sending, setSending] = useState(false);
  const [failed, setFailed] = useState(false);
  const [duration, setDuration] = useState(124);
  return (
    <Card>
      <CardHeader>
        <CardTitle>API Client</CardTitle>
        <CardDescription>
          Local samples: parameters, response and retry states.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <Button variant="outline" onClick={() => setVariablesOpen(true)}>
          Preview environment editor
        </Button>
        <Dialog open={variablesOpen} onOpenChange={setVariablesOpen}>
          <DialogContent className="flex max-h-[90dvh] flex-col overflow-hidden sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>Environment editor</DialogTitle>
              <DialogDescription>
                Local layout preview. Values are not saved.
              </DialogDescription>
            </DialogHeader>
            <div className="min-h-0 overflow-auto">
              <VariablesForm
                rows={variables}
                onRowsChange={setVariables}
                environmentDetails
                name={envName}
                onNameChange={setEnvName}
                tag={envTag}
                onTagChange={setEnvTag}
                envTags={[
                  {
                    id: "staging",
                    name: "staging",
                    requiresConfirmation: false,
                    isBuiltin: true,
                    locked: false,
                  },
                  {
                    id: "prod",
                    name: "prod",
                    requiresConfirmation: true,
                    isBuiltin: true,
                    locked: false,
                  },
                ]}
              />
            </div>
          </DialogContent>
        </Dialog>
        <Button variant="outline" onClick={() => setImportOpen(true)}>
          Preview import dialog
        </Button>
        <ImportDialogView
          open={importOpen}
          kind={kind}
          onKindChange={setKind}
          text={text}
          onTextChange={setText}
          fileName={fileName}
          onFile={(value, name) => {
            setText(value);
            setFileName(name);
          }}
          format={format}
          onFormatChange={setFormat}
          formats={[]}
          tag={tag}
          onTagChange={setTag}
          tags={[]}
          error={error}
          onError={setError}
          busy={false}
          onClose={() => { setImportOpen(false); setError(""); }}
          onSubmit={() => setError("Preview only — no data has been imported.")}
        />
        <div className="overflow-hidden rounded-xl border border-border-subtle">
          <RequestToolbar
            name={name}
            onNameChange={(value) => {
              setName(value);
              setDirty(true);
            }}
            dirty={dirty}
            folders={[{ id: "users", label: "Platform / Users" }]}
            folderId={folder}
            onFolderChange={setFolder}
            environments={[
              { id: "staging", label: "API staging" },
              { id: "prod", label: "API production" },
            ]}
            environmentId={environment}
            onEnvironmentChange={setEnvironment}
            environmentTag={
              <span className="rounded bg-state-soft px-1.5 py-0.5 text-caption-11">
                {environment}
              </span>
            }
            gated={environment === "prod"}
            onManageEnvironment={() => setVariablesOpen(true)}
            url={url}
            onUrlChange={setUrl}
            method={method}
            onMethodChange={setMethod}
            sending={sending}
            onSend={() => setSending(true)}
            onCancel={() => setSending(false)}
            onSave={() => setDirty(false)}
            onCurl={() => {}}
          />
          <Tabs defaultValue="params" className="gap-3 px-4 pt-2 pb-4">
            <TabsList variant="line">
              <TabsTrigger value="params">Params</TabsTrigger>
              <TabsTrigger value="headers">Headers</TabsTrigger>
            </TabsList>
            <TabsContent value="params" className="space-y-3">
              <p className="text-caption-11 text-text-tertiary">
                Query parameters are appended to the URL. Uncheck a row to keep
                it out of this request.
              </p>
              <KeyValueTable
                rows={rows}
                onChange={setRows}
                keyPlaceholder="Parameter"
              />
            </TabsContent>
            <TabsContent value="headers">
              <KeyValueTable
                rows={[{ key: "Accept", value: "application/json" }]}
                onChange={() => {}}
                keyPlaceholder="Header"
              />
            </TabsContent>
          </Tabs>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setSending((value) => !value)}
          >
            Toggle sending sample
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setFailed((value) => !value)}
          >
            Toggle error sample
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setDuration((value) => (value === 124 ? 89 : 124))}
          >
            Change sample timing
          </Button>
        </div>
        <div className="flex h-80 min-h-0 flex-col rounded-lg border border-border-subtle">
          <ResponsePane
            response={{
              status: 200,
              statusText: "200 OK",
              headers: {
                "Content-Type": "application/json",
                "X-Request-Id": "preview",
              },
              body: '{"items":[{"id":1,"name":"Sample"}],"total":1}',
              truncated: false,
              bytes: 55,
              durationMs: duration,
            }}
            sending={sending}
            error={failed ? "Connection refused" : ""}
            curl=""
            onCloseCurl={() => {}}
            requestId=""
            sends={1}
          />
        </div>
      </CardContent>
    </Card>
  );
}
