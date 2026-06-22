/**
 * Layout for the public, signed-out surface (the welcome page). It is deliberately
 * chrome-free — no app header — so the marketing/sign-in content owns the whole
 * viewport and centers itself. The signed-in platform uses the (session) layout
 * instead, which carries the shared header.
 */
export default function PublicLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <div className="flex h-full flex-1 flex-col">{children}</div>;
}
