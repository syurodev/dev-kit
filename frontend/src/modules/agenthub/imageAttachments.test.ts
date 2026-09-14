import { describe, expect, it } from "bun:test";

import { imageAttachmentFromFile, MAX_AGENT_IMAGE_BYTES } from "./imageAttachments";

describe("image attachments", () => {
  it("rejects unsupported formats and oversized images before reading them", async () => {
    await expect(imageAttachmentFromFile(new File(["x"], "icon.bmp", { type: "image/bmp" }))).rejects.toThrow("PNG");
    await expect(imageAttachmentFromFile(new File([new Uint8Array(MAX_AGENT_IMAGE_BYTES + 1)], "huge.png", { type: "image/png" }))).rejects.toThrow("3.75 MB");
  });

  it("allows exactly as many bytes as fit Claude's 5 MB base64 image limit", () => {
    // The limit is on the encoded payload; the file size the composer sees is
    // the decoded one. The old 8 MiB decoded limit was over 10 MiB of base64.
    const base64Length = (bytes: number) => Math.ceil(bytes / 3) * 4;
    expect(base64Length(MAX_AGENT_IMAGE_BYTES)).toBe(5 << 20);
    expect(base64Length(MAX_AGENT_IMAGE_BYTES + 1)).toBeGreaterThan(5 << 20);
  });
});
