import type { Category, Product } from "pgdatalayer";
import rawCategories from "pgdatalayer/data/categories.json";
import rawProducts from "pgdatalayer/data/products.json";

/**
 * The faux catalog, read from the pgdatalayer package at build time.
 *
 * The JSON is validated against its schema in PGDataLayer's CI, so the casts
 * only narrow what TypeScript infers from JSON (e.g. `currency: string`) to the
 * types generated from those same schemas.
 */
export const categories: Category[] = rawCategories as Category[];
export const products: Product[] = rawProducts as Product[];

export interface CategoryGroup {
  category: Category;
  products: Product[];
}

/**
 * One group per category, in category order, with each group's products sorted
 * by name. Products whose category is not listed are left out. Inputs are not
 * mutated.
 */
export function groupProductsByCategory(
  categoryList: readonly Category[],
  productList: readonly Product[],
): CategoryGroup[] {
  const byCategory = new Map<string, Product[]>();
  for (const category of categoryList) byCategory.set(category.id, []);
  for (const product of productList) byCategory.get(product.categoryId)?.push(product);

  return categoryList.map((category) => ({
    category,
    products: (byCategory.get(category.id) ?? []).sort((a, b) => a.name.localeCompare(b.name)),
  }));
}

const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });

/** Formats integer cents as US dollars, e.g. 1299 → "$12.99". */
export function formatPrice(cents: number): string {
  return usd.format(cents / 100);
}
