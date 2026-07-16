import { FolderOpen } from "lucide-react";

export function ProjectRequiredEmpty({ resource }: { resource: string }) {
  return (
    <div className="rounded-lg border border-dashed border-border p-12 text-center bg-muted/20">
      <FolderOpen className="h-10 w-10 mx-auto text-muted-foreground mb-3" />
      <h2 className="text-lg font-semibold mb-1">Выберите проект</h2>
      <p className="text-sm text-muted-foreground">
        {resource} — проектный ресурс. Используйте селектор в шапке или зайдите на страницу
        <code className="mx-1 px-1.5 py-0.5 bg-muted rounded text-xs">/iam/projects</code>и нажмите{" "}
        <span className="font-medium">Выбрать</span>.
      </p>
    </div>
  );
}
