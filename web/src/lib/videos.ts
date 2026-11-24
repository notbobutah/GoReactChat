/** One film in the portfolio, as GET /api/videos returns it. */
export type Video = {
  id: string;
  title: string;
  description: string;
  /** Public object URL. Playback goes straight to the store. */
  url: string;
  sizeBytes: number;
  /** RFC 3339, UTC. */
  lastModified: string;
};

/** What loadVideos settles on. */
export type LoadedVideos = { status: "ready"; videos: Video[] } | { status: "empty" } | { status: "error" };

/** What the page can be showing. */
export type VideosState = { status: "loading" } | LoadedVideos;

// Same-origin by default, for the reasons given in chat-client.ts.
const baseUrl = process.env.NEXT_PUBLIC_LUMI_URL ?? "";

/** Never rejects: every failure, including an abort, is the error state. */
export async function loadVideos(signal: AbortSignal): Promise<LoadedVideos> {
  try {
    const res = await fetch(`${baseUrl}/api/videos`, { signal });
    if (!res.ok) return { status: "error" };
    const body = (await res.json()) as { videos?: Video[] } | null;
    const videos = body?.videos;
    if (!Array.isArray(videos)) return { status: "error" };
    return videos.length > 0 ? { status: "ready", videos } : { status: "empty" };
  } catch {
    return { status: "error" };
  }
}

/** The API redirects this to a short-lived attachment URL. */
export function downloadUrl(id: string): string {
  return `${baseUrl}/api/videos/${encodeURIComponent(id)}/download`;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/**
 * "22 Aug 2026", in UTC. Spelled out rather than left to Intl so it matches the
 * date the server puts in default titles; en-GB, for one, abbreviates to "Sept".
 */
export function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
}
