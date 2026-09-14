import { describe, expect, it } from "bun:test";

import { imageAttachmentFromFile, MAX_AGENT_IMAGE_BYTES } from "./imageAttachments";

describe("image attachments", () => {
  it("rejects unsupported formats and oversized images before reading them", async () => {
    await expect(imageAttachmentFromFile(new File(["x"], "icon.bmp", { type: "image/bmp" }))).rejects.toThrow("PNG");
    await expect(imageAttachmentFromFile(new File([new Uint8Array(MAX_AGENT_IMAGE_BYTES + 1)], "huge.png", { type: "image/png" }))).rejects.toThrow("8 MB");
  });
});
