import type { Metadata } from "next";
import Link from "next/link";

import { categories, formatPrice, groupProductsByCategory, products } from "@/lib/catalog";

export const metadata: Metadata = {
  title: "Catalog",
  description: "A small faux product catalog, read at build time from the PGDataLayer data package.",
};

// A server component with no request-time APIs: the catalog is bundled from the
// pgdatalayer package, so the whole page is prerendered at build time.
export default function CatalogPage() {
  const groups = groupProductsByCategory(categories, products);

  return (
    <main className="mx-auto w-full max-w-6xl space-y-8 px-6 py-10">
      <header className="space-y-3">
        <Link href="/" className="text-xs text-black/50 hover:text-black dark:text-white/50 dark:hover:text-white">
          ← back to the chat
        </Link>
        <h1 className="text-2xl font-medium">Catalog</h1>
        <p className="max-w-3xl text-sm leading-relaxed text-black/70 dark:text-white/70">
          {products.length} faux products across {categories.length} categories, read at build time from the PGDataLayer
          data package.
        </p>
      </header>

      {groups.map(({ category, products: items }) => (
        <section key={category.id} aria-labelledby={`category-${category.id}`} className="space-y-3">
          <div className="space-y-1">
            <h2 id={`category-${category.id}`} className="text-xs font-medium tracking-wide text-black/50 uppercase dark:text-white/50">
              {category.name}
            </h2>
            {category.description && (
              <p className="text-sm text-black/70 dark:text-white/70">{category.description}</p>
            )}
          </div>

          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {items.map((p) => (
              <li key={p.id} className="h-full rounded-lg border border-black/10 px-3 py-2 dark:border-white/15">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm font-medium">{p.name}</span>
                  <span className="text-sm tabular-nums text-black/70 dark:text-white/70">{formatPrice(p.priceCents)}</span>
                </div>
                <p className="mt-1 text-xs leading-relaxed text-black/60 dark:text-white/60">{p.description}</p>
                {!p.inStock && (
                  <span className="mt-2 inline-block rounded-full bg-black/[0.06] px-2 py-0.5 text-[10px] text-black/60 dark:bg-white/10 dark:text-white/60">
                    Out of stock
                  </span>
                )}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </main>
  );
}
