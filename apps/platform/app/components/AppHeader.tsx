import AppLogo from "./AppLogo";

/**
 * The shared platform top bar: the Octo mark on the left (linking back to the
 * dashboard), a flexible middle slot for page controls, and the account tile on
 * the right. The dashboard and file manager render this directly; the editor's
 * EditorHeader is a richer variant that composes the same AppLogo + account tile
 * so the logo and user badge are consistent on every screen (issue #48).
 *
 * The middle slot owns its own horizontal layout (it is a `flex-1` flex row), so
 * a page can push an action to the far right with `ml-auto`.
 */
export default function AppHeader({
  logoHref,
  userMenu,
  children,
}: {
  /** Override the logo target; defaults to the dashboard. `null` makes it inert. */
  logoHref?: string | null;
  /** Account control slot (server-rendered UserMenu); only visible when SSO is on. */
  userMenu?: React.ReactNode;
  /** Middle controls (tabs, actions) placed between the logo and the account tile. */
  children?: React.ReactNode;
}) {
  return (
    <header className="flex items-center gap-3 border-b border-black/10 px-4 h-12 shrink-0 dark:border-white/10">
      <AppLogo href={logoHref} />
      {children != null && (
        <div className="flex min-w-0 flex-1 items-center gap-3">{children}</div>
      )}
      <div className="ml-auto flex items-center gap-2">{userMenu}</div>
    </header>
  );
}
