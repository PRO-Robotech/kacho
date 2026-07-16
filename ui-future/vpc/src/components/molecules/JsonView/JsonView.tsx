export function JsonView({ data }: { data: unknown }) {
  return (
    <pre className="rounded-md bg-zinc-950 text-zinc-100 p-4 text-xs overflow-auto leading-relaxed">
      <code>{JSON.stringify(data, null, 2)}</code>
    </pre>
  );
}
