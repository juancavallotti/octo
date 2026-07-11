import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

// GitHub Pages serves the site under /octo; override for other hosts.
const basePath = process.env.DOCS_BASE_PATH ?? '/octo';

/** @type {import('next').NextConfig} */
const config = {
  output: 'export',
  basePath,
  env: {
    NEXT_PUBLIC_BASE_PATH: basePath,
  },
  images: { unoptimized: true },
  trailingSlash: true,
  reactStrictMode: true,
};

export default withMDX(config);
