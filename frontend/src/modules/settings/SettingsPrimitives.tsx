import { cn } from "@/lib/utils";

/**
 * The column every tab's content sits in: 656 wide, which is the Figma
 * shell's 940 less the 220 nav and the 32 gutters.
 *
 * Declared once because it was declared five times, and five copies of a
 * measurement is five chances for one of them to be 640 after somebody
 * nudges a tab.
 */
export function TabColumn({ gap = 6, children }: { gap?: 6 | 7; children: React.ReactNode }) {
  return (
    <div className={cn("flex w-full max-w-164 flex-col", gap === 7 ? "gap-7" : "gap-6")}>
      {children}
    </div>
  );
}

/** Every section opens the same way (765:2000–2003): Heading/24 (Figma draws
 * 22 SemiBold, snapped up to the scale), 6px, Body/13 tertiary, 28px. */
export function SectionHeader({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <h2 className="text-heading-24 text-text-primary">{title}</h2>
      <p className="text-body-14 text-text-tertiary">{description}</p>
    </div>
  );
}

/** THEME / CODE & TERMINAL — Label/11 Caps, tertiary, 10px above its control. */
export function GroupLabel({ children }: { children: React.ReactNode }) {
  return <p className="text-label-11-caps text-text-tertiary">{children}</p>;
}
