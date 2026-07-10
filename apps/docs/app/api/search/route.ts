import { source } from '@/lib/source';
import { createFromSource } from 'fumadocs-core/search/server';

// Static export: the index is generated at build time and served as a file.
export const revalidate = false;

export const { staticGET: GET } = createFromSource(source);
