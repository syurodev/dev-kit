import type { AgentImageAttachment } from "@/bridge/agenthub";

export const MAX_AGENT_IMAGE_BYTES = 8 << 20;
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
  if (file.size > MAX_AGENT_IMAGE_BYTES) throw new Error("Images must be 8 MB or smaller.");
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
