import { FolderOpen } from "lucide-react";

export function ProjectRequiredEmpty({ resource }: { resource: string }) {
  return (
    // Радиус 11 — ряд поверхности: это пустая панель на месте таблицы, а не
    // элемент управления.
    <div className="rounded-[11px] border border-dashed border-[var(--kc-border)] bg-[var(--kc-field)] p-12 text-center">
      <FolderOpen className="h-10 w-10 mx-auto text-muted-foreground mb-3" />
      <h2 className="text-lg font-semibold mb-1">Выберите проект</h2>
      <p className="text-sm text-muted-foreground">
        {resource} — проектный ресурс. Используйте селектор в шапке или зайдите на страницу
        <code className="t-mono mx-1 rounded-[5px] border border-[var(--kc-border)] bg-[var(--kc-field)] px-1.5 py-0.5">
          /iam/projects
        </code>
        и нажмите <span className="font-medium">Выбрать</span>.
      </p>
    </div>
  );
}
