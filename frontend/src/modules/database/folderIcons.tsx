import {
  Folder,
  Server,
  Laptop,
  Database,
  Monitor,
  Layers,
  Clock,
  Hash,
  Tag,
  Terminal,
  User,
  SlidersHorizontal,
  FileText,
  Link,
  Palette,
  Plus,
} from "lucide-react";
import { PostgresMark, MySQLMark, MariaDBMark, SQLiteMark } from "./icons/engineMarks";

type IconComponent = (props: { className?: string }) => React.ReactNode;

/**
 * The twenty icons of Figma `DB / Folder Icon Picker` (532:2726), in the order
 * the grid shows them. A closed list on purpose: the stored value comes back
 * from ui_state and is used to look up a component, so an unknown string —
 * hand-edited, from an older build, or a Lucide rename — has to land somewhere
 * safe rather than break the sidebar.
 */
export const FOLDER_ICONS: ReadonlyArray<{ name: string; Icon: IconComponent }> = [
  { name: "folder", Icon: Folder },
  { name: "server", Icon: Server },
  { name: "laptop", Icon: Laptop },
  { name: "database", Icon: Database },
  { name: "monitor", Icon: Monitor },
  { name: "layers", Icon: Layers },
  { name: "clock", Icon: Clock },
  { name: "hash", Icon: Hash },
  { name: "tag", Icon: Tag },
  { name: "terminal", Icon: Terminal },
  { name: "user", Icon: User },
  { name: "sliders-horizontal", Icon: SlidersHorizontal },
  { name: "file-text", Icon: FileText },
  { name: "link", Icon: Link },
  { name: "palette", Icon: Palette },
  { name: "plus", Icon: Plus },
  { name: "db-postgres", Icon: PostgresMark },
  { name: "db-mysql", Icon: MySQLMark },
  { name: "db-mariadb", Icon: MariaDBMark },
  { name: "db-sqlite", Icon: SQLiteMark },
];

export const DEFAULT_FOLDER_ICON = "folder";

export function folderIconComponent(name: string | undefined): IconComponent {
  return FOLDER_ICONS.find((i) => i.name === name)?.Icon ?? Folder;
}

export function FolderIcon({ name, className }: { name?: string; className?: string }) {
  const Icon = folderIconComponent(name);
  return <Icon className={className} />;
}
