import { readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { generateManifest } from "material-icon-theme";
import type { Plugin } from "vite";

/** Keep only file associations in the client; SVGs stay separately cached assets. */
export function materialFileIcons(): Plugin {
  const require = createRequire(import.meta.url);
  const root = path.dirname(require.resolve("material-icon-theme/package.json"));
  const manifest = generateManifest();
  const data = {
    file: manifest.file!,
    fileNames: manifest.fileNames ?? {},
    fileExtensions: manifest.fileExtensions ?? {},
    light: {
      fileNames: manifest.light?.fileNames ?? {},
      fileExtensions: manifest.light?.fileExtensions ?? {},
    },
  };
  const generated = path.resolve(import.meta.dirname, "../src/components/file-icon/associations.json");
  const json = JSON.stringify(data) + "\n";
  // Committed so unit tests can resolve file types without starting Vite.
  if (readFileSync(generated, "utf8") !== json) writeFileSync(generated, json);
  const ids = new Set([
    data.file,
    ...Object.values(data.fileNames), ...Object.values(data.fileExtensions),
    ...Object.values(data.light.fileNames), ...Object.values(data.light.fileExtensions),
  ]);
  const assets = new Map<string, Buffer>([...ids].map((id) => {
    const definition = manifest.iconDefinitions?.[id];
    if (!definition?.iconPath) throw new Error(`Missing Material file icon: ${id}`);
    return [`${id}.svg`, readFileSync(path.resolve(root, "dist", definition.iconPath))] as const;
  }));
  assets.set("LICENSE", readFileSync(path.join(root, "LICENSE")));
  return {
    name: "material-file-icons",
    configureServer(server) {
      server.middlewares.use("/file-icons", (request, response, next) => {
        const name = request.url?.split("?")[0]?.slice(1) ?? "";
        const asset = assets.get(name);
        if (!asset) return next();
        response.setHeader("Content-Type", name.endsWith(".svg") ? "image/svg+xml" : "text/plain");
        response.end(asset);
      });
    },
    generateBundle() {
      for (const [name, source] of assets) this.emitFile({ type: "asset", fileName: `file-icons/${name}`, source });
    },
  };
}
