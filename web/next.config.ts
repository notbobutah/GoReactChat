import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Standalone output traces the exact files the server needs and emits a
  // self-contained server.js, so the runtime image ships no node_modules tree
  // and no build toolchain. Without this the image would carry the whole
  // dependency graph to run one page.
  output: "standalone",
};

export default nextConfig;
