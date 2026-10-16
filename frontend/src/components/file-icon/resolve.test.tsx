import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { resolveFileIcon } from "./resolve";
import { FileIcon } from "./FileIcon";
import { materialFileIcons } from "../../../scripts/material-file-icons";

describe("Material file icons", () => {
  test("recognizes names, case, platform paths and compound extensions", () => {
    for (const [file, icon] of [
      ["src/Main.java", "java"], ["CONFIG.YAML", "yaml"], ["config.yml", "yaml"],
      ["C:\\src\\Main.java", "java"], ["Dockerfile", "docker"],
      ["docker-compose.yaml", "docker"], ["README.md", "readme"],
      ["App.tsx", "react_ts"], ["model.d.ts", "typescript-def"],
      ["model.test.ts", "test-ts"], [".gitignore", "git"],
      ["unknown.zzzunknown", "file"], ["", "file"], ["constructor", "file"],
    ]) expect(resolveFileIcon(file)).toBe(icon);
  });

  test("uses the light association only when it exists", () => {
    expect(resolveFileIcon("config.toml")).toBe("toml");
    expect(resolveFileIcon("config.toml", true)).toBe("toml_light");
    expect(resolveFileIcon("Main.java", true)).toBe("java");
  });

  test("renders decorative local SVGs and switches theme without JS subscriptions", () => {
    const html = renderToStaticMarkup(<FileIcon path="config.toml" />);
    expect(html).toContain('aria-hidden="true"');
    expect(html).toContain('alt=""');
    expect(html).toContain('/file-icons/toml.svg');
    expect(html).toContain('/file-icons/toml_light.svg');
    expect(html).toContain('dark:hidden');
    expect(html).toContain('dark:block');
  });

  test("packages every resolved SVG plus its license, outside the JS bundle", async () => {
    const plugin = materialFileIcons();
    const emitted: { fileName: string; source: Uint8Array }[] = [];
    const hook = plugin.generateBundle as Function;
    await hook.call({ emitFile: (asset: typeof emitted[number]) => emitted.push(asset) });
    expect(emitted.length).toBeGreaterThan(500);
    expect(emitted.some((asset) => asset.fileName === 'file-icons/LICENSE')).toBe(true);
    for (const file of ["Main.java", "a.yaml", "Dockerfile", "config.toml"]) {
      for (const light of [false, true]) {
        const asset = emitted.find((asset) => asset.fileName === `file-icons/${resolveFileIcon(file, light)}.svg`);
        expect(asset).toBeDefined();
        expect(Buffer.from(asset!.source).toString()).toContain('<svg');
      }
    }
  });
});
