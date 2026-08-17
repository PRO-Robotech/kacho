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

/**
 * Отказ сервера на ВИД «контейнер» — прочитанный из дерева, а не пересказанный.
 *
 * Тот же приём самоистечения, что у `localDiskRefusal`, и по той же причине.
 * Контракт объявляет `InstanceKind.CONTAINER` несоздаваемым (у образа из
 * реестра нет неизменяемого адреса: репозиторий адресуется парой «реестр +
 * имя», а имя меняется отдельным глаголом), поэтому форма обязана сказать это
 * словами ДО отправки — иначе арендатор получает отказ на запрос, который не
 * мог пройти.
 *
 * Основание держится ФАКТОМ О ДЕРЕВЕ: станет вид создаваемым — отказ исчезнет,
 * и проба, ссылающаяся на него, покраснеет сама, не дожидаясь, пока кто-нибудь
 * вспомнит про клиентскую половину.
 *
 * Читается ИМЕННО отказ по виду: у поля `instance_kind` в том же файле есть и
 * другой отказ («вид обязателен»), и спутать их значило бы получить зелёную
 * пробу на дереве, где отказа по виду уже нет.
 */
export function containerKindRefusal(): string | null {
  const path = join(REPO_ROOT, "services", "compute", "internal", "apps", "kacho", "api", "instance", "instance.go");
  const text = readFileSync(path, "utf8");
  const m = /serviceerr\.InvalidArg\("instance_kind",\s*\n?\s*"([^"]*not creatable[^"]*)"/.exec(text);
  return m ? m[1] : null;
}
