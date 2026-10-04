export function validateConnection(form: { name: string; host: string; port: string; username: string; env: string; secret: string }, editing: boolean) {
  const errors: Partial<Record<keyof typeof form, string>> = {};
  if (!form.name.trim()) errors.name = "Enter a connection name.";
  if (!form.host.trim()) errors.host = "Enter the server host.";
  if (!form.username.trim()) errors.username = "Enter a username.";
  if (!form.env) errors.env = "Choose an environment.";
  const port = Number(form.port);
  if (!form.port.trim() || !Number.isInteger(port) || port < 1 || port > 65535) errors.port = "Enter a port from 1 to 65535.";
  if (!editing && !form.secret.trim()) errors.secret = "Enter the password or private key.";
  return errors;
}

export function validateForward(spec: { listenPort: string | number; targetPort: string | number; targetHost: string }) {
  const errors: Record<string, string> = {};
  for (const [key, min] of [["listenPort", 0], ["targetPort", 1]] as const) {
    const raw = String(spec[key]);
    const value = Number(raw);
    if (!raw.trim() || !Number.isInteger(value) || value < min || value > 65535) errors[key] = `Enter a port from ${min} to 65535.`;
  }
  if (!spec.targetHost.trim()) errors.targetHost = "Enter the destination host.";
  return errors;
}
