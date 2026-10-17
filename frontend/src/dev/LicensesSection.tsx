import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LicensesSetting } from "@/modules/settings/LicensesSetting";

const client = new QueryClient();

/** Settings → Open source licenses, as the page draws it. `?gallery#settings-licenses`. */
export function LicensesSection() {
  return (
    <section id="settings-licenses" className="flex flex-col gap-6 p-6">
      <div className="flex flex-col gap-1.5">
        <h2 className="text-heading-24 text-text-primary">Open source licenses</h2>
        <p className="text-body-14 text-text-tertiary">Third-party notices for software included in DevKit.</p>
      </div>
      <div className="w-full max-w-164">
        <QueryClientProvider client={client}>
          <LicensesSetting />
        </QueryClientProvider>
      </div>
    </section>
  );
}
