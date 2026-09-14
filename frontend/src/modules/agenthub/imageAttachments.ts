import type { AgentImageAttachment } from "@/bridge/agenthub";

/**
 * Claude limits an image by its base64 payload: 10 MB on the direct API, 5 MB
 * on Bedrock and Vertex, and DevKit cannot tell which the agent is signed in
 * to. 5 MiB of base64 is exactly this many decoded bytes (3.75 MiB). Mirrors
 * maxImageBase64Bytes in internal/agenthub/attachment.go.
 */
export const MAX_AGENT_IMAGE_BYTES = ((5 << 20) / 4) * 3;
export const AGENT_IMAGE_TYPES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

function readAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error("Could not read image"));
    reader.readAsDataURL(file);
  });
}

export async function imageAttachmentFromFile(file: File): Promise<AgentImageAttachment> {
  if (!AGENT_IMAGE_TYPES.has(file.type)) throw new Error("Use a PNG, JPEG, GIF, or WebP image.");
  if (file.size > MAX_AGENT_IMAGE_BYTES) throw new Error("Images must be 3.75 MB or smaller.");
  const previewUrl = await readAsDataURL(file);
  const comma = previewUrl.indexOf(",");
  if (comma < 0) throw new Error("Could not read image");
  return {
    id: `${file.name}-${file.lastModified}-${file.size}`,
    name: file.name || "pasted-image",
    mimeType: file.type,
    data: previewUrl.slice(comma + 1),
    previewUrl,
    size: file.size,
  };
}
