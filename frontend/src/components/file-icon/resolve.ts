import associations from "./associations.json";

type Associations = Record<string, string>;
const names: Associations = associations.fileNames;
const extensions: Associations = associations.fileExtensions;
const lightNames: Associations = associations.light.fileNames;
const lightExtensions: Associations = associations.light.fileExtensions;
const own = (map: Associations, key: string) => Object.prototype.hasOwnProperty.call(map, key) ? map[key] : undefined;

/** Exact names win; compound suffixes (test.ts, d.ts, …) win over short ones. */
export function resolveFileIcon(path: string, light = false): string {
  const name = path.replace(/\\/g, "/").split("/").pop()?.toLowerCase() ?? "";
  const exact = (light && own(lightNames, name)) || own(names, name);
  if (exact) return exact;
  for (let dot = name.indexOf("."); dot >= 0; dot = name.indexOf(".", dot + 1)) {
    const suffix = name.slice(dot + 1);
    const icon = (light && own(lightExtensions, suffix)) || own(extensions, suffix);
    if (icon) return icon;
  }
  return associations.file;
}
