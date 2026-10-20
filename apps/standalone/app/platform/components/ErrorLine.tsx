/** What a call to the shell said when it failed, under the control that made it. */
export default function ErrorLine({ error }: { error: string | null }) {
  if (!error) return null;
  return (
    <p role="alert" className="whitespace-pre-line text-xs text-rose-600 dark:text-rose-400">
      {error}
    </p>
  );
}
