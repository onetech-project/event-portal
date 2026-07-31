import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emits .next/standalone with a minimal server.js and only the node_modules
  // actually reached, so the runtime image needs no npm install.
  output: "standalone",
};

export default nextConfig;
