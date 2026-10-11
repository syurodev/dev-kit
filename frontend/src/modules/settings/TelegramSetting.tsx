import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { errorMessage } from "../../bridge/errors";
import { listWorkspaces } from "../../bridge/workspace";
import {
  telegramStatus, configureTelegram, beginTelegramPair, acceptTelegramPair,
  unpairTelegram, type TelegramConfig,
} from "../../bridge/telegram";

const KEY = ["settings", "telegram"] as const;
export function TelegramSetting() {
  const client = useQueryClient();
  const { data, error } = useQuery({ queryKey: KEY, queryFn: telegramStatus, refetchInterval: 2000 });
  const { data: workspaces } = useQuery({ queryKey: ["workspaces"], queryFn: listWorkspaces });
  const [draft, setDraft] = useState<TelegramConfig | null>(null);
  const [token, setToken] = useState("");
  const [pairLink, setPairLink] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState("");
  const cfg = draft ?? data?.config;
  async function perform(action: () => Promise<void>) {
    setBusy(true); setFailed("");
    try { await action(); await client.invalidateQueries({ queryKey: KEY }); }
    catch (cause) { setFailed(errorMessage(cause)); }
    finally { setBusy(false); }
  }
  const message = failed || (error ? errorMessage(error) : "") || data?.error;
  if (!cfg) return <p className="text-body-12 text-text-tertiary">{message || "Đang tải Telegram…"}</p>;
  function toggle(key: "enabled" | "sendDetails" | "readTranscript" | "approveInteractions", label: string) {
    return <label className="flex items-center justify-between gap-3 text-body-13">
      <span>{label}</span><Switch checked={cfg?.[key] ?? false} disabled={busy} onCheckedChange={(value) => setDraft({ ...cfg!, [key]: value })} />
    </label>;
  }
  return <div className="flex flex-col gap-3 rounded-control border border-border-subtle bg-surface-raised p-3 text-text-primary">
    <p className="text-body-12 text-text-tertiary">Bot riêng cho máy này. DevKit phải đang chạy và vault đã mở khóa. Nội dung được gửi qua dịch vụ Telegram.</p>
    {toggle("enabled", "Bật điều khiển qua Telegram")}
    <label className="flex flex-col gap-1 text-body-12">Token bot từ BotFather (để trống để giữ token đã lưu)
      <Input type="password" autoComplete="new-password" value={token} disabled={busy} onChange={(e) => setToken(e.target.value)} placeholder={cfg.accountId ? "Đã lưu token mã hóa cục bộ" : "Nhập token của bot riêng"} />
    </label>
    <div className="flex flex-col gap-2">
      <span className="text-body-12 text-text-tertiary">Workspace cho phép điều khiển agent</span>
      {(workspaces ?? []).map((w) => <label key={w.id} className="flex items-center gap-2 text-body-12">
        <input type="checkbox" checked={(cfg.workspaces ?? []).includes(w.id)} disabled={busy} onChange={(e) => setDraft({ ...cfg, workspaces: e.target.checked ? [...(cfg.workspaces ?? []), w.id] : (cfg.workspaces ?? []).filter((id) => id !== w.id) })} />
        <span>{w.name} <span className="text-text-tertiary">({w.id})</span></span>
      </label>)}
    </div>
    {toggle("sendDetails", "Gửi nội dung yêu cầu quyền và câu hỏi để trả lời")}
    {toggle("readTranscript", "Cho phép đọc nội dung session qua /read")}
    {toggle("approveInteractions", "Cho phép duyệt xác nhận từ UI/terminal trên toàn máy")}
    <p className="text-body-12 text-text-tertiary">Xác nhận từ agent giới hạn theo workspace đã chọn. Xác nhận UI/terminal áp dụng trên toàn máy khi bật tùy chọn riêng; không ghi nhớ quyền lâu dài.</p>
    <Button disabled={busy} onClick={() => void perform(async () => { await configureTelegram(cfg, token); setToken(""); setDraft(null); setPairLink(""); })}>Lưu cấu hình</Button>
    {data?.paired ? <div className="flex items-center justify-between gap-2 text-body-12">
      <span>Đã ghép Telegram user {data.userId}</span>
      <Button variant="outline" disabled={busy} onClick={() => void perform(async () => { await unpairTelegram(); setPairLink(""); })}>Hủy ghép</Button>
    </div> : <Button variant="outline" disabled={busy || !data?.config.enabled || draft !== null} onClick={() => void perform(async () => { setPairLink(await beginTelegramPair()); })}>Tạo liên kết ghép tài khoản</Button>}
    {pairLink ? <div className="flex flex-col gap-1 text-body-12"><span>Mở liên kết trong Telegram rồi bấm Start. Liên kết hết hạn sau 5 phút.</span><a href={pairLink} target="_blank" rel="noreferrer" className="break-all text-text-link">{pairLink}</a></div> : null}
    {data?.candidateId ? <div className="flex flex-col gap-2 text-body-12">
      <span>Yêu cầu ghép từ @{data.candidateName || "không có username"} · user {data.candidateId}. Chỉ xác nhận nếu đúng tài khoản của bạn.</span>
      <Button disabled={busy} onClick={() => void perform(async () => { await acceptTelegramPair(data.candidateId); setPairLink(""); })}>Xác nhận ghép tài khoản này</Button>
    </div> : null}
    <p className="text-body-12 text-text-tertiary">Sau khi ghép: /sessions để chọn, /read để đọc, /older để xem lịch sử, /models để chuyển model, /stop để dừng. Reply đúng tin hỏi để trả lời agent.</p>
    {message ? <p role="alert" className="text-body-12 text-status-danger">{message}</p> : null}
  </div>;
}
