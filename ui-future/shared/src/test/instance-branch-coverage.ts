// Ветви создания машины — общая часть проб обоих реестров, объявляющих её
// (`@shared` и `compute`). Форма машины живёт в двух местах, и предмет #375 в
// том и состоит, что правка одного места до другого не доезжает.

import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

const REPO_ROOT = resolve(process.cwd(), "../..");

/** Спек, от которого пробе нужно только приведение тела. */
export interface SanitizeCarrier {
  sanitize?: (obj: Record<string, unknown>) => Record<string, unknown>;
}

/** Минимально законное тело создания машины выбранного вида. */
export function instanceBody(spec: SanitizeCarrier, kind: "VM" | "CONTAINER"): Record<string, unknown> {
  return spec.sanitize!({
    project_id: "prj-1",
    zone_id: "ru-a",
    machine_type_id: "mt-1",
    instance_kind: kind,
    boot_source: { type: "storage.image", id: "img-1" },
    vm_spec: { user_data: "#cloud-config" },
    container_spec: { restart_policy: "NEVER", working_dir: "/srv" },
  });
}

/**
 * Отказ сервера, из-за которого ветвь `AttachedLocalDiskSpec.type` из консоли
 * недостижима, — прочитанный из дерева, а не пересказанный здесь.
 *
 * Это ПРЕДИКАТ СНЯТИЯ послабления: ветвь не выражена формой не потому, что о
 * ней забыли, а потому, что её родительское поле сервер отвергает синхронно,
 * называя его (второй из трёх законных исходов). Исчезнет отказ — исчезнет и
 * основание не иметь формы, и проба скажет об этом сама.
 */
export function localDiskRefusal(): string | null {
  const path = join(REPO_ROOT, "services", "compute", "internal", "handler", "instance_handler.go");
  const text = readFileSync(path, "utf8");
  const m = /add\("local_disk_specs",\s*\n?\s*"([^"]+)"/.exec(text);
  return m ? m[1] : null;
}
