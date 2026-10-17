import { useQuery } from "@tanstack/react-query";
import { readableLicenses } from "./thirdPartyLicenses";
// An emitted asset, not a JS chunk: the text is large and only this page fetches it.
import licensesUrl from "../../../../THIRD_PARTY_LICENSES.md?url";

/**
 * The open-source notices for what ships inside DevKit, read from the generated
 * THIRD_PARTY_LICENSES.md at the repository root. It is fetched on demand: the text is
 * large and only this page wants it.
 */
export function LicensesSetting() {
  const { data, error, isPending } = useQuery({
    queryKey: ["settings", "third-party-licenses"],
    queryFn: async () => {
      const response = await fetch(licensesUrl);
      if (!response.ok) throw new Error(`licenses: ${response.status}`);
      return readableLicenses(await response.text());
    },
    staleTime: Infinity,
  });

  if (error) {
    return (
      <p className="text-body-12 text-status-danger">
        The notices could not be loaded. They are also in THIRD_PARTY_LICENSES.md in the DevKit repository.
      </p>
    );
  }
  return (
    <div className="rounded-control border border-border-subtle bg-surface-raised p-4">
      {isPending ? (
        <p className="text-body-12 text-text-tertiary">Loading…</p>
      ) : (
        <pre className="text-code-11 wrap-break-word whitespace-pre-wrap text-text-secondary select-text">{data}</pre>
      )}
    </div>
  );
}
