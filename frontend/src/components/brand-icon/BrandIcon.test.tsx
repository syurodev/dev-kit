import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { BrandIcon, brandName, isBrand, type BrandId } from "./BrandIcon";

const BRANDS: BrandId[] = ["kong", "kafka", "github", "gitlab", "bitbucket"];

describe("BrandIcon", () => {
  test("every brand draws one path on the surrounding colour", () => {
    for (const brand of BRANDS) {
      const html = renderToString(<BrandIcon brand={brand} />);
      expect(html).toContain(`data-brand="${brand}"`);
      expect(html).toContain('fill="currentColor"');
      expect(html.match(/<path /g)?.length).toBe(1);
    }
  });

  test("it is decorative: the name is in the row, not a second tooltip", () => {
    const html = renderToString(<BrandIcon brand="kong" />);
    expect(html).toContain('aria-hidden="true"');
    expect(html).not.toContain("<title>Kong");
  });

  test("16px by default, and a caller's size or colour wins", () => {
    expect(renderToString(<BrandIcon brand="gitlab" />)).toContain("size-4");
    const html = renderToString(<BrandIcon brand="gitlab" className="size-3 text-text-tertiary" />);
    expect(html).toContain("text-text-tertiary");
    expect(html).toContain("size-3");
  });

  test("names are the brands' own", () => {
    expect(brandName("kafka")).toBe("Apache Kafka");
    expect(brandName("github")).toBe("GitHub");
  });

  test("isBrand answers for a git provider id and refuses anything else", () => {
    for (const brand of BRANDS) expect(isBrand(brand)).toBe(true);
    for (const other of ["custom", "", "toString", "constructor", "__proto__", null, undefined]) {
      expect(isBrand(other as string | null | undefined)).toBe(false);
    }
  });
});
