import type { CreateProfileInput } from "@/bridge/database";

export type ConnectionField =
  "name" | "host" | "port" | "username" | "env" | "sshProfileId";
export type ConnectionErrors = Partial<Record<ConnectionField, string>>;

/** Validate the exact target before either Test or Save; the backend remains authoritative. */
export function validateConnection(
  input: CreateProfileInput,
  environments: readonly string[],
  sshIds: readonly string[],
): ConnectionErrors {
  const errors: ConnectionErrors = {};
  if (!input.name.trim()) errors.name = "Enter a connection name.";
  if (!input.host.trim()) errors.host = "Enter a host or server address.";
  if (!Number.isInteger(input.port) || input.port < 1 || input.port > 65535)
    errors.port = "Use a port from 1 to 65535.";
  if (!input.username.trim()) errors.username = "Enter a username.";
  if (!environments.includes(input.env))
    errors.env = "Choose an available environment.";
  if (input.sshProfileId && !sshIds.includes(input.sshProfileId))
    errors.sshProfileId = "Choose an available SSH profile.";
  return errors;
}
