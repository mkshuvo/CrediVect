import type { NextConfig } from "next";

// The browser only ever calls this UI origin. Requests to /api/v1/* are proxied
// server-side to the core service, so the app works no matter which host port
// core is published on (or whether it is published at all).
const CORE_ORIGIN = process.env.CORE_ORIGIN || "http://localhost:8000";

const nextConfig: NextConfig = {
  output: "standalone",
  reactStrictMode: true,
  async rewrites() {
    return [
      {
        source: "/api/v1/:path*",
        destination: `${CORE_ORIGIN}/api/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
