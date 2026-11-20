import type { Metadata } from "next";

import { PipelineView } from "@/components/pipeline/pipeline-view";
import { DEFAULT_FEED_URL } from "@/lib/pipeline-feed";

export const metadata: Metadata = {
  title: "Agentic pipeline · Rob MacKay",
  description:
    "Watch AI agents take a tracker card from plan to pull request: the developer approves the plan, agents write code and tests, the pipeline verifies every commit.",
};

// The page shell is static; the feed is fetched in the browser from the
// published snapshot, so a new pipeline run never needs a redeploy.
export default function PipelinePage() {
  return <PipelineView feedUrl={process.env.NEXT_PUBLIC_PIPELINE_FEED_URL ?? DEFAULT_FEED_URL} />;
}
