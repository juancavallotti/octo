/** What the panel says before anybody has asked him anything. */
export default function EmptyTranscript() {
  return (
    <div className="m-auto max-w-[22rem] text-center text-xs text-zinc-500">
      <p>
        Ask about this installation — an integration, a deployment that will not
        start, or a flow you are writing.
      </p>
      <p className="mt-2">He knows which page you are on, and can take you to another one.</p>
    </div>
  );
}
