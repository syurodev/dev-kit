import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { KeyValueTable } from "@/modules/apiclient/KeyValueTable";
import { ResponsePane } from "@/modules/apiclient/ResponsePane";
import type { KV } from "@/bridge/apiclient";

/** UI fixtures only. No request is sent and no history is read. */
export function ApiClientCalmPreview() {
  const [rows, setRows] = useState<KV[]>([{ key: "limit", value: "20" }, { key: "sort", value: "created_at", disabled: true }]);
  const [sending, setSending] = useState(false);
  const [failed, setFailed] = useState(false);
  const [duration, setDuration] = useState(124);
  return <Card><CardHeader><CardTitle>API Client</CardTitle><CardDescription>Local samples: parameters, response and retry states.</CardDescription></CardHeader>
    <CardContent className="space-y-3"><KeyValueTable rows={rows} onChange={setRows} keyPlaceholder="Parameter" />
      <div className="flex flex-wrap gap-2"><Button variant="outline" size="sm" onClick={() => setSending((value) => !value)}>Toggle sending sample</Button><Button variant="outline" size="sm" onClick={() => setFailed((value) => !value)}>Toggle error sample</Button><Button variant="outline" size="sm" onClick={() => setDuration((value) => value === 124 ? 89 : 124)}>Change sample timing</Button></div>
      <div className="flex h-80 min-h-0 flex-col rounded-lg border border-border-subtle"><ResponsePane response={{ status: 200, statusText: "200 OK", headers: { "Content-Type": "application/json", "X-Request-Id": "preview" }, body: '{"items":[{"id":1,"name":"Sample"}],"total":1}', truncated: false, bytes: 55, durationMs: duration }} sending={sending} error={failed ? "Connection refused" : ""} curl="" onCloseCurl={() => {}} requestId="" sends={1} /></div>
    </CardContent>
  </Card>;
}
