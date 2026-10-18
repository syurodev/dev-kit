import SiApachekafka from "@icons-pack/react-simple-icons/icons/SiApachekafka";
import SiBitbucket from "@icons-pack/react-simple-icons/icons/SiBitbucket";
import SiGithub from "@icons-pack/react-simple-icons/icons/SiGithub";
import SiGitlab from "@icons-pack/react-simple-icons/icons/SiGitlab";
import SiKong from "@icons-pack/react-simple-icons/icons/SiKong";
import { cn } from "@/lib/utils";

/**
 * The brands DevKit draws, from Simple Icons (@icons-pack/react-simple-icons).
 *
 * Add a brand here, one import per icon: the package has ~7000 and the barrel
 * would pull a module per icon into the dev server, while a deep import ships
 * exactly the ones named below. Every mark is a single path, drawn on
 * currentColor — the chrome is monochrome, and a brand's own colour is not a
 * status or a palette hue (the Database module's engine marks are the exception
 * Figma asks for; they live with it, not here).
 *
 * Not for files: an icon for a file by its name or extension is the file-icon
 * set (components/file-icon), which has light and dark variants.
 */
const BRANDS = {
  kong: { name: "Kong", Icon: SiKong },
  kafka: { name: "Apache Kafka", Icon: SiApachekafka },
  github: { name: "GitHub", Icon: SiGithub },
  gitlab: { name: "GitLab", Icon: SiGitlab },
  bitbucket: { name: "Bitbucket", Icon: SiBitbucket },
} as const;

export type BrandId = keyof typeof BRANDS;

/** Whether DevKit has a mark for this id (a git provider id, a vendor, …). */
export function isBrand(id: string | null | undefined): id is BrandId {
  return typeof id === "string" && Object.prototype.hasOwnProperty.call(BRANDS, id);
}

/** The brand's own name, for a label or a tooltip. */
export function brandName(brand: BrandId): string {
  return BRANDS[brand].name;
}

/**
 * A brand's mark: decorative by default (the name is written beside it), 16px,
 * on the surrounding text colour. A caller that sizes its icons (a tree row
 * does) wins, as with the lucide icons.
 */
export function BrandIcon({ brand, className }: { brand: BrandId; className?: string }) {
  const { Icon } = BRANDS[brand];
  // `title=""`: the package puts a <title> in every mark, which would be a
  // second tooltip on a mark whose name is already in the row.
  return <Icon title="" aria-hidden="true" data-brand={brand} className={cn("size-4 shrink-0", className)} />;
}
