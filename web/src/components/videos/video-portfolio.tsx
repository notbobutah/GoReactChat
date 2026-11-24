"use client";

import { useEffect, useState } from "react";

import { VideoGrid } from "@/components/videos/video-grid";
import { loadVideos, type VideosState } from "@/lib/videos";

/** Loads the listing in the browser and hands its state to the grid. */
export function VideoPortfolio() {
  const [state, setState] = useState<VideosState>({ status: "loading" });

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      const loaded = await loadVideos(controller.signal);
      if (!controller.signal.aborted) setState(loaded);
    })();
    return () => controller.abort();
  }, []);

  return <VideoGrid state={state} />;
}
