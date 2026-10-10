import path from "node:path";
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The docs pages read ../../docs at build time; trace from the repo root.
  outputFileTracingRoot: path.join(__dirname, "../.."),
};

export default nextConfig;
