import { BrandIcon, brandName, type BrandId } from "@/components/brand-icon/BrandIcon";

const BRANDS: BrandId[] = ["kong", "kafka", "github", "gitlab", "bitbucket"];

/** Simple Icons marks on currentColor: at the sizes DevKit draws them, in the colours it draws them in. */
export function BrandIconsSection() {
  return (
    <section id="brand-icons" className="flex flex-col gap-4">
      <h2 className="text-title-16 text-text-primary">Brand marks</h2>
      <div className="grid gap-4 rounded-control bg-surface-base p-4 sm:grid-cols-2">
        {BRANDS.map((brand) => (
          <div key={brand} className="flex items-center gap-4 text-code-12 text-text-primary">
            <span className="w-28">{brandName(brand)}</span>
            <BrandIcon brand={brand} className="size-3.5 text-icon-secondary" />
            <BrandIcon brand={brand} className="text-text-tertiary" />
            <BrandIcon brand={brand} className="size-6 text-text-primary" />
            <BrandIcon brand={brand} className="text-status-success" />
          </div>
        ))}
      </div>
      <p className="text-caption-11 text-text-tertiary">Tree icon (14px), row (16px), heading (24px), and a state colour (Kafka: connected).</p>
    </section>
  );
}
