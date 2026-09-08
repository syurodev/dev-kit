/**
 * "2m", "4h", "3d", "2w" — the compact form the Figma note rows and cards
 * carry (813:3500, 798:3178). Callers add " ago" where the design does.
 */
export function relativeTime(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime();
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;
  const week = 7 * day;
  if (diffMs < hour) return `${Math.max(1, Math.round(diffMs / minute))}m`;
  if (diffMs < day) return `${Math.round(diffMs / hour)}h`;
  if (diffMs < week) return `${Math.round(diffMs / day)}d`;
  return `${Math.round(diffMs / week)}w`;
}
