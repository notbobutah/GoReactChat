import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import type { Category, Product } from "pgdatalayer";
import rawCategories from "pgdatalayer/data/categories.json";
import rawProducts from "pgdatalayer/data/products.json";
import {
  categories,
  formatPrice,
  groupProductsByCategory,
  products,
} from "@/lib/catalog";

function product(
  id: string,
  name: string,
  categoryId: string,
  overrides: Partial<Product> = {},
): Product {
  return {
    id,
    name,
    categoryId,
    priceCents: 1000,
    currency: "USD",
    description: `${name} description`,
    inStock: true,
    ...overrides,
  };
}

const fixtureCategories: Category[] = [
  { id: "tools", name: "Tools", description: "Hand tools." },
  { id: "garden", name: "Garden" },
  { id: "books", name: "Books", description: "Paper things." },
];

const fixtureProducts: Product[] = [
  product("trowel", "Trowel", "garden"),
  product("novel", "Novel", "books"),
  product("wrench", "Wrench", "tools"),
  product("atlas", "Atlas", "books"),
  product("hammer", "Hammer", "tools", { inStock: false }),
  product("hose", "Hose", "garden"),
  product("chisel", "Chisel", "tools"),
];

describe("groupProductsByCategory (fixtures)", () => {
  const groups = groupProductsByCategory(fixtureCategories, fixtureProducts);

  it("returns one group per category, in the given category order", () => {
    expect(groups.map((g) => g.category.id)).toEqual(["tools", "garden", "books"]);
  });

  it("keeps the full category object on each group", () => {
    expect(groups[0].category).toEqual(fixtureCategories[0]);
    expect(groups[1].category).toEqual(fixtureCategories[1]);
    expect(groups[2].category).toEqual(fixtureCategories[2]);
  });

  it("puts each product under its own category, sorted by name", () => {
    expect(groups[0].products.map((p) => p.id)).toEqual(["chisel", "hammer", "wrench"]);
    expect(groups[1].products.map((p) => p.id)).toEqual(["hose", "trowel"]);
    expect(groups[2].products.map((p) => p.id)).toEqual(["atlas", "novel"]);
    for (const group of groups) {
      for (const p of group.products) {
        expect(p.categoryId).toBe(group.category.id);
      }
    }
  });

  it("includes every product exactly once and preserves product fields", () => {
    const flattened = groups.flatMap((g) => g.products);
    expect(flattened).toHaveLength(fixtureProducts.length);
    expect(new Set(flattened.map((p) => p.id)).size).toBe(fixtureProducts.length);
    expect(flattened.find((p) => p.id === "hammer")).toEqual(fixtureProducts[4]);
  });

  it("does not mutate its inputs", () => {
    const cats = structuredClone(fixtureCategories);
    const prods = structuredClone(fixtureProducts);
    groupProductsByCategory(cats, prods);
    expect(cats).toEqual(fixtureCategories);
    expect(prods).toEqual(fixtureProducts);
  });
});

describe("formatPrice", () => {
  it("formats integer cents as US dollars", () => {
    expect(formatPrice(1299)).toBe("$12.99");
    expect(formatPrice(500)).toBe("$5.00");
    expect(formatPrice(5)).toBe("$0.05");
    expect(formatPrice(0)).toBe("$0.00");
  });
});

describe("catalog data from the pgdatalayer package", () => {
  it("exposes the package's categories and products unchanged", () => {
    expect(categories).toEqual(rawCategories);
    expect(products).toEqual(rawProducts);
  });

  it("has 5 categories and about 25 products", () => {
    expect(categories).toHaveLength(5);
    expect(products.length).toBeGreaterThanOrEqual(20);
    expect(products.length).toBeLessThanOrEqual(30);
  });

  it("groups every real product exactly once under an existing category", () => {
    const groups = groupProductsByCategory(categories, products);
    expect(groups.map((g) => g.category.id)).toEqual(categories.map((c) => c.id));

    const flattened = groups.flatMap((g) => g.products);
    expect(flattened).toHaveLength(products.length);
    expect(new Set(flattened.map((p) => p.id))).toEqual(new Set(products.map((p) => p.id)));

    for (const group of groups) {
      expect(group.products.length).toBeGreaterThan(0);
      for (const p of group.products) {
        expect(p.categoryId).toBe(group.category.id);
      }
      const names = group.products.map((p) => p.name);
      expect(names).toEqual([...names].sort((a, b) => a.localeCompare(b)));
    }
  });

  it("formats every real price as a dollar string", () => {
    for (const p of products) {
      expect(formatPrice(p.priceCents)).toMatch(/^\$\d{1,3}(,\d{3})*\.\d{2}$/);
    }
  });
});

describe("pgdatalayer dependency pin", () => {
  it("is installed from GitHub at a full 40-character commit SHA", () => {
    const pkgPath = fileURLToPath(new URL("../../package.json", import.meta.url));
    const pkg = JSON.parse(readFileSync(pkgPath, "utf8")) as {
      dependencies?: Record<string, string>;
    };
    expect(pkg.dependencies?.pgdatalayer).toMatch(
      /^github:notbobutah\/PGDataLayer#[0-9a-f]{40}$/,
    );
  });
});
