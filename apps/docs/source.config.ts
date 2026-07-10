import { defineConfig, defineDocs, frontmatterSchema } from 'fumadocs-mdx/config';
import { z } from 'zod';

export const docs = defineDocs({
  dir: 'content/docs',
  docs: {
    schema: frontmatterSchema.extend({
      // Runtime type strings (connectors, blocks, sources) documented by this
      // page. scripts/check-docs-drift.mjs compares the union of these against
      // `octo schema` output in CI.
      octo_types: z.array(z.string()).optional(),
    }),
  },
});

export default defineConfig();
