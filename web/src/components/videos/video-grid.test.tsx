import type { ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeAll, describe, expect, it, vi } from "vitest";

import { VideoGrid } from "@/components/videos/video-grid";

const ORIGIN = "https://expona-images.us-lax-1.linodeobjects.com";

const videos = [
  {
    id: "0b9d3c1e-5a52-4b7e-9a41-2f6f6f1d7c01",
    title: "Harbour lights",
    description: "A night crossing.",
    url: `${ORIGIN}/videos/0b9d3c1e-5a52-4b7e-9a41-2f6f6f1d7c01/cinematic-graded.mp4`,
    sizeBytes: 12345678,
    lastModified: "2026-08-22T10:00:00Z",
  },
  {
    id: "1c8e4d2f-6b63-4c8f-8b52-3a7a7a2e8d02",
    title: "Expona film · 5 Jul 2026",
    description: "",
    url: `${ORIGIN}/videos/1c8e4d2f-6b63-4c8f-8b52-3a7a7a2e8d02/cinematic.mp4`,
    sizeBytes: 2345678,
    lastModified: "2026-07-05T23:30:00Z",
  },
];

// Downloads are same-origin unless a base is configured; keep the developer's
// own environment out of these assertions.
vi.hoisted(() => vi.stubEnv("NEXT_PUBLIC_LUMI_URL", undefined));

function render(state: ComponentProps<typeof VideoGrid>["state"]): string {
  return renderToStaticMarkup(<VideoGrid state={state} />);
}

function text(html: string): string {
  return html.replace(/<[^>]*>/g, " ");
}

function count(html: string, pattern: RegExp): number {
  return [...html.matchAll(pattern)].length;
}

describe("VideoGrid ready", () => {
  let html = "";
  let players: string[] = [];
  beforeAll(() => {
    html = render({ status: "ready", videos });
    players = html.match(/<video\b[\s\S]*?<\/video>/g) ?? [];
  });

  it("renders one player per video, in order, at the public url", () => {
    expect(players).toHaveLength(videos.length);
    videos.forEach((v, i) => expect(players[i]).toContain(`src="${v.url}"`));
  });

  it("gives every player controls, playsInline and preload=metadata, without autoplay", () => {
    expect(players).toHaveLength(videos.length);
    for (const player of players) {
      const tag = player.slice(0, player.indexOf(">"));
      expect(tag).toMatch(/\scontrols(=|\s|$)/);
      expect(tag).toMatch(/\splaysinline(=|\s|$)/i);
      expect(tag).toContain('preload="metadata"');
      expect(tag).not.toMatch(/autoplay/i);
    }
  });

  it("keeps players 16:9 and portrait-safe", () => {
    expect(html).toContain("aspect-video");
    expect(players).toHaveLength(videos.length);
    for (const player of players) expect(player).toContain("object-contain");
  });

  it("is a responsive grid with dark variants", () => {
    expect(html).toMatch(/class="[^"]*\bgrid\b[^"]*\b(sm|md|lg):grid-cols-\d/);
    expect(html).toContain("dark:");
  });

  it("shows each title, description and date", () => {
    const visible = text(html);
    expect(visible).toContain("Harbour lights");
    expect(visible).toContain("A night crossing.");
    expect(visible).toContain("22 Aug 2026");
    expect(visible).toContain("Expona film · 5 Jul 2026");
    // The second date, separately from the same words in its default title.
    expect(visible.replace("Expona film · 5 Jul 2026", "")).toContain("5 Jul 2026");
  });

  it("links a Download anchor for each video to the API", () => {
    const links = html.match(/<a\b[\s\S]*?<\/a>/g) ?? [];
    for (const v of videos) {
      const link = links.find((a) => a.includes(`href="/api/videos/${v.id}/download"`));
      expect(link).toBeDefined();
      expect(text(link ?? "")).toContain("Download");
    }
    expect(count(html, /href="\/api\/videos\/[^"]*\/download"/g)).toBe(videos.length);
  });

  it("shows no loading, empty or error message", () => {
    expect(text(html)).not.toMatch(/loading|no (videos|films)/i);
    expect(html).not.toContain('role="alert"');
  });
});

describe("VideoGrid without videos", () => {
  it("renders a loading state", () => {
    const html = render({ status: "loading" });
    expect(text(html)).toMatch(/loading/i);
    expect(html).not.toContain("<video");
    expect(html).not.toContain('role="alert"');
    expect(text(html)).not.toMatch(/no (videos|films)/i);
  });

  it("renders an empty state", () => {
    const html = render({ status: "empty" });
    expect(text(html)).toMatch(/no (videos|films)/i);
    expect(html).not.toContain("<video");
    expect(html).not.toContain('role="alert"');
    expect(text(html)).not.toMatch(/loading/i);
  });

  it("renders an error state as an alert", () => {
    const html = render({ status: "error" });
    expect(html).toContain('role="alert"');
    expect(text(html).trim()).not.toBe("");
    expect(html).not.toContain("<video");
    expect(text(html)).not.toMatch(/loading|no (videos|films)/i);
  });
});
