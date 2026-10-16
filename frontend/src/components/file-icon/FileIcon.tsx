import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { resolveFileIcon } from "./resolve";

/** Material Icon Theme assets are bundled by Vite and work entirely offline. */
export function FileIcon({ path, className, ...props }: Omit<ComponentProps<"span">, "children"> & { path: string }) {
  const dark = resolveFileIcon(path);
  const light = resolveFileIcon(path, true);
  const image = (icon: string, visibility?: string) => (
    <img src={`/file-icons/${icon}.svg`} alt="" draggable={false} className={cn("size-full object-contain", visibility)} />
  );
  return (
    <span {...props} aria-hidden="true" data-file-icon={dark} className={cn("inline-flex size-4 shrink-0 align-middle pointer-events-none", className)}>
      {dark === light ? image(dark) : <>{image(light, "dark:hidden")}{image(dark, "hidden dark:block")}</>}
    </span>
  );
}

/** For APIs whose icon slot accepts a component rather than an element. */
export function fileIconComponent(path: string) {
  return function MaterialFileIcon(props: { className?: string }) {
    return <FileIcon path={path} {...props} />;
  };
}
