import { callBridge } from "./client";
import { AppService } from "../../bindings/changeme/internal/bridge";

export type SSHProfile = {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  authMethod: string;
  hostKeyFingerprint: string;
};

export type CreateSSHProfileInput = {
  name: string;
  host: string;
  port: number;
  username: string;
  authMethod: string;
  secret: string;
};

function toProfile(p: {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  auth_method: string;
  host_key_fingerprint: string;
}): SSHProfile {
  return {
    id: p.id,
    name: p.name,
    host: p.host,
    port: p.port,
    username: p.username,
    authMethod: p.auth_method,
    hostKeyFingerprint: p.host_key_fingerprint,
  };
}

export function listSSHProfiles(): Promise<SSHProfile[]> {
  return callBridge(async () => {
    const list = await AppService.SSHListProfiles();
    return (list ?? []).map(toProfile);
  });
}

export function createSSHProfile(input: CreateSSHProfileInput): Promise<SSHProfile> {
  return callBridge(async () => {
    const created = await AppService.SSHCreateProfile({
      name: input.name,
      host: input.host,
      port: input.port,
      username: input.username,
      auth_method: input.authMethod,
      secret: input.secret,
    });
    return toProfile(created);
  });
}

export function deleteSSHProfile(id: string): Promise<void> {
  return callBridge(() => AppService.SSHDeleteProfile(id) as Promise<void>);
}

export function observeSSHHostKey(id: string): Promise<string> {
  return callBridge(() => AppService.SSHObserveHostKey(id) as Promise<string>);
}

export function trustSSHHostKey(id: string, fingerprint: string): Promise<void> {
  return callBridge(() => AppService.SSHTrustHostKey(id, fingerprint) as Promise<void>);
}

export function testSSHConnection(id: string, passphrase: string): Promise<void> {
  return callBridge(() => AppService.SSHTestConnection(id, passphrase) as Promise<void>);
}

export function runSSHCommand(id: string, command: string, passphrase: string): Promise<string> {
  return callBridge(async () => {
    const result = await AppService.SSHRunCommand(id, command, passphrase);
    return result.output;
  });
}
