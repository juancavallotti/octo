/**
 * Layout for the public, signed-out surface. Chrome-free — no app header — so the
 * sign-in content owns the whole viewport and centers itself.
 */
export default function PublicLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <div className="flex h-full flex-1 flex-col">{children}</div>;
}
