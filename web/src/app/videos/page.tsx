import type { Metadata } from "next";
import Link from "next/link";

import { VideoPortfolio } from "@/components/videos/video-portfolio";

export const metadata: Metadata = {
  title: "Expona portfolio",
  description: "Cinematic marketing films made with Expona, to watch and download.",
};

// A static shell: the listing is fetched in the browser, so the page prerenders
// at build time without reaching the API.
export default function VideosPage() {
  return (
    <main className="mx-auto w-full max-w-6xl space-y-8 px-6 py-10">
      <header className="space-y-3">
        <Link href="/" className="text-xs text-black/50 hover:text-black dark:text-white/50 dark:hover:text-white">
          ← back to the chat
        </Link>
        <h1 className="text-2xl font-medium">Expona portfolio</h1>
        <p className="max-w-3xl text-sm leading-relaxed text-black/70 dark:text-white/70">
          Cinematic marketing films made with Expona, to watch and download.
        </p>
      </header>

      <VideoPortfolio />
    </main>
  );
}
