import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { OCTO_VERSION } from '@/lib/version';

const basePath = process.env.NEXT_PUBLIC_BASE_PATH ?? '';

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src={`${basePath}/octo.png`}
            alt=""
            width={24}
            height={24}
            className="rounded-sm"
          />
          <span className="font-semibold">Octo</span>
          <span className="text-xs text-fd-muted-foreground font-normal">
            v{OCTO_VERSION}
          </span>
        </>
      ),
    },
    githubUrl: 'https://github.com/juancavallotti/octo',
  };
}
