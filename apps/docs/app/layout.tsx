import './global.css';
import { RootProvider } from 'fumadocs-ui/provider/next';
import { Inter } from 'next/font/google';
import type { ReactNode } from 'react';

const inter = Inter({
  subsets: ['latin'],
});

export const metadata = {
  title: {
    template: '%s | Octo',
    default: 'Octo Documentation',
  },
  description:
    'Documentation for Octo, the AI-native integration platform: runtime, connectors, visual editor, and the self-hosted iPaaS.',
};

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className={inter.className} suppressHydrationWarning>
      <body className="flex flex-col min-h-screen">
        <RootProvider
          search={{
            options: {
              type: 'static',
              api: `${process.env.NEXT_PUBLIC_BASE_PATH ?? ''}/api/search`,
            },
          }}
        >
          {children}
        </RootProvider>
      </body>
    </html>
  );
}
