import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const SRC = join(import.meta.dir, "../..");
const read = (rel: string) => readFileSync(join(SRC, rel), "utf8");

/**
 * A CA bundle only means something under a mode that verifies one. The backend
 * refuses the other pairing (ErrCACertificateWithoutTLS), so the dialog must
 * not be able to send it — otherwise the user meets a rejection for a field the
 * form let them fill in.
 */
describe("CA certificate field", () => {
  const dialog = read("modules/database/components/NewConnectionDialog.tsx");

  it("is only offered under a verifying SSL mode", () => {
    expect(dialog).toContain("verifiesCertificate");
    expect(dialog).toMatch(/verifies\s*&&/);
  });

  it("never submits a bundle the chosen mode would ignore", () => {
    expect(dialog).toContain("const caForSubmit = verifies ? caCert : \"\"");
    // The submitted value is the guarded one, not the raw state.
    expect(dialog).toContain("caCert: caForSubmit");
    expect(dialog).not.toMatch(/caCert:\s*caCert\b/);
  });

  it("explains what each SSL mode actually guarantees", () => {
    for (const mode of ["disable", "require", "verify-full"]) {
      expect(dialog).toContain(`${mode.includes("-") ? `"${mode}"` : mode}:`);
    }
    expect(dialog).toContain("SSL_MODE_HINT[sslMode]");
  });
});

describe("SSH key passphrase field", () => {
  const view = read("modules/ssh/SSHView.tsx");

  it("appears only for key auth", () => {
    expect(view).toContain('form.authMethod === "key" && (');
    expect(view).toContain("ssh-key-passphrase");
  });

  it("is part of the form the profile is created from", () => {
    expect(view).toContain("keyPassphrase:");
  });
});

describe("the bridge carries both new fields", () => {
  it("database sends and reads ca_cert", () => {
    const bridge = read("bridge/database.ts");
    expect(bridge).toContain("ca_cert: input.caCert");
    expect(bridge).toContain("ca_cert: opts.caCert");
    expect(bridge).toContain("caCert: p.ca_cert");
  });

  it("ssh sends key_passphrase and reads has_key_passphrase", () => {
    const bridge = read("bridge/ssh.ts");
    expect(bridge).toContain("key_passphrase: input.keyPassphrase");
    expect(bridge).toContain("hasKeyPassphrase: Boolean(p.has_key_passphrase)");
  });
});
