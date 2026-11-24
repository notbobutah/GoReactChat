import { downloadUrl, formatDate, type Video, type VideosState } from "@/lib/videos";

/** Presentational: renders whichever state the loader is in. */
export function VideoGrid({ state }: { state: VideosState }) {
  switch (state.status) {
    case "loading":
      return (
        <p role="status" className="text-sm text-black/50 dark:text-white/50">
          Loading films…
        </p>
      );
    case "empty":
      return <p className="text-sm text-black/50 dark:text-white/50">No films to show yet.</p>;
    case "error":
      return (
        <p
          role="alert"
          className="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300"
        >
          The portfolio is unavailable right now. Please try again in a few minutes.
        </p>
      );
    case "ready":
      return (
        <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {state.videos.map((v) => (
            <VideoCard key={v.id} video={v} />
          ))}
        </ul>
      );
  }
}

function VideoCard({ video }: { video: Video }) {
  return (
    <li className="flex h-full flex-col overflow-hidden rounded-lg border border-black/10 dark:border-white/15">
      {/* object-contain: a portrait film is letterboxed in the 16:9 frame, not cropped. */}
      <video
        src={video.url}
        controls
        playsInline
        preload="metadata"
        aria-label={video.title}
        className="aspect-video w-full bg-black object-contain"
      />
      <div className="flex flex-1 flex-col gap-1 px-3 py-2">
        <h2 className="text-sm font-medium">{video.title}</h2>
        {video.description && (
          <p className="text-xs leading-relaxed text-black/60 dark:text-white/60">{video.description}</p>
        )}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <time dateTime={video.lastModified} className="text-xs text-black/50 dark:text-white/50">
            {formatDate(video.lastModified)}
          </time>
          {/* A plain anchor: next/link would prefetch the API route. */}
          <a
            href={downloadUrl(video.id)}
            aria-label={`Download ${video.title}`}
            className="rounded-lg bg-black px-3 py-1.5 text-xs text-white dark:bg-white dark:text-black"
          >
            Download
          </a>
        </div>
      </div>
    </li>
  );
}
