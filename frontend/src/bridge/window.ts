import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

export function getTitlebarInsetHeight(): Promise<number> {
  return callBridge(() => AppService.TitlebarInsetHeight());
}
