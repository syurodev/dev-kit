import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

export type TelegramConfig = {
  enabled: boolean;
  accountId: string;
  workspaces: string[];
  sendDetails: boolean;
  readTranscript: boolean;
  approveInteractions: boolean;
};
export type TelegramStatus = {
  config: TelegramConfig;
  paired: boolean;
  userId: number;
  candidateId: number;
  candidateName: string;
  botName: string;
  error: string;
};
export function telegramStatus(): Promise<TelegramStatus> {
  return callBridge(async () => JSON.parse(await AppService.TelegramStatus()) as TelegramStatus);
}
export function configureTelegram(config: TelegramConfig, token: string): Promise<void> {
  return callBridge(() => AppService.TelegramConfigure(JSON.stringify(config), token));
}
export function beginTelegramPair(): Promise<string> {
  return callBridge(() => AppService.TelegramBeginPair());
}
export function acceptTelegramPair(userId: number): Promise<void> {
  return callBridge(() => AppService.TelegramAcceptPair(userId));
}
export function unpairTelegram(): Promise<void> {
  return callBridge(() => AppService.TelegramUnpair());
}
