import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TelegramConfig, TelegramStatus } from "@/bridge/telegram";
import type { AgentSession } from "@/bridge/agenthub";
import { SessionRemoteControlItem } from "./SessionRemoteControlItem";
import type { MenuKit } from "./actions";
const Item: MenuKit["Item"] = ({disabled,children}) => <button disabled={disabled}>{children}</button>;
const session: AgentSession = {id:"a",workspaceId:"ws",agentType:"codex",projectPath:"/project",status:"idle",parentSessionId:null,depth:0,title:"Task",acpSessionId:"",updatedAt:0};
function render(config: Partial<TelegramConfig>) {
 const status: TelegramStatus = {config:{enabled:true,accountId:"",workspaces:["ws"],sendDetails:true,readTranscript:true,approveInteractions:false,...config},paired:true,userId:7,candidateId:0,candidateName:"",botName:"",error:""};
 const client=new QueryClient();
 client.setQueryData(["settings","telegram","session-preview"],status);
 return renderToString(<QueryClientProvider client={client}><SessionRemoteControlItem session={session} Item={Item} api={{status:async()=>status,setEnabled:async()=>{}}}/></QueryClientProvider>);
}
test("legacy session membership defaults off even with workspace permission",()=>{
 const html=render({});expect(html).toContain("Enable remote control");expect(html).not.toContain("disabled");
});
test("missing/null workspace grants disable enabling without crashing the menu",()=>{
 expect(render({workspaces:null as unknown as string[]})).toContain("disabled");
});
test("an opted-in session can be disabled after its workspace grant is removed",()=>{
 const html=render({sessionIds:["a"],workspaces:[]});expect(html).toContain("Disable remote control");expect(html).not.toContain("disabled");
});
test("another session's opt-in never enables this session",()=>{
 expect(render({sessionIds:["other"]})).toContain("Enable remote control");
});
