import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emits .next/standalone for the Docker image.
  output: "standalone",
};

export default nextConfig;
