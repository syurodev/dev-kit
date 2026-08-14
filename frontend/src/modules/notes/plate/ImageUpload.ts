import { ImagePlugin } from "@platejs/media/react";
import { toast } from "sonner";

// Matches Plate's own default image cap (4MB/image) — enforced explicitly
// here so an oversized image is rejected with a clear error instead of
// silently accepted and only failing (or bloating the note) later.
const MAX_IMAGE_BYTES = 4 * 1024 * 1024;

/**
 * Local-first: there is no server to upload to. Plate's uploadImage option
 * accepts a Data URL and may return the original input unchanged — that's
 * exactly what happens here. The data URI becomes the image node's `url`
 * and gets embedded directly in the note's markdown body, inside the same
 * encrypted payload as everything else. No separate asset store, no new
 * decrypt/serve path.
 */
export const ImageUploadPlugin = ImagePlugin.configure({
  options: {
    uploadImage: async (dataUrl: ArrayBuffer | string) => {
      if (typeof dataUrl === "string" && dataUrl.startsWith("data:")) {
        const approxBytes = Math.ceil((dataUrl.length * 3) / 4);
        if (approxBytes > MAX_IMAGE_BYTES) {
          toast.error("Image exceeds the 4MB limit and was not inserted.");
          throw new Error("Image exceeds the 4MB limit and was not inserted.");
        }
      }
      return dataUrl;
    },
  },
});
