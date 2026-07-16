// DeleteButton — DELETE /v1/<plural>/{id} → Operation → poll до done.

import { useCallback, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { Button } from "@shared/components/atoms/ui/Button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@shared/components/atoms/ui/Dialog";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { OperationToastWatcher } from "@shared/components/molecules/OperationToastWatcher";
import { ApiError, api } from "@shared/api/client";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

interface Props {
  /** /v1/<plural>/{id} */
  apiPath: string;
  /** ID ресурса в registry — для инвалидации кэша */
  resourceId: string;
  name: string;
  resourceLabel: string;
  projectId?: string | null;
  triggerLabel?: string;
  /** После успешного удаления (например, navigate на список) */
  navigateTo?: () => void;
}

export function DeleteButton({ apiPath, resourceId, name, resourceLabel, projectId, triggerLabel, navigateTo }: Props) {
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [opId, setOpId] = useState<string | null>(null);

  const invalidate = useInvalidateResourceList();

  const mutation = useMutation({
    mutationFn: () => api.delete(apiPath),
    onSuccess: (resp) => {
      setErr(null);
      setOpen(false);
      const id = extractOperationId(resp);
      if (id) {
        setOpId(id);
      } else {
        // Без Operation — синхронный success
        invalidate(resourceId, projectId ?? null);
        navigateTo?.();
      }
    },
    onError: (e) => {
      const m = e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message;
      setErr(m);
      toast.error(`Delete ${resourceLabel} ${name}: ${m}`);
    },
  });

  const handleOperationDone = useCallback(
    (success: boolean) => {
      setOpId(null);
      invalidate(resourceId, projectId ?? null);
      if (success) navigateTo?.();
    },
    [invalidate, resourceId, projectId, navigateTo],
  );

  return (
    <>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger asChild>
          <Button variant="destructive" size="sm">
            <Trash2 className="h-4 w-4" />
            {triggerLabel ?? "Delete"}
          </Button>
        </DialogTrigger>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Удалить {resourceLabel}?</DialogTitle>
            <DialogDescription>
              <span className="font-medium text-foreground">{name}</span>
              <br />
              <code className="text-xs text-muted-foreground">{apiPath}</code>
              <br />
              Действие необратимо.
            </DialogDescription>
          </DialogHeader>
          {err && <div className="rounded-md bg-destructive/10 text-destructive p-2 text-xs">{err}</div>}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={mutation.isPending}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
              {mutation.isPending ? "Deleting…" : "Delete"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <OperationToastWatcher opId={opId} title={`Deleting ${resourceLabel} ${name}`} onDone={handleOperationDone} />
    </>
  );
}
