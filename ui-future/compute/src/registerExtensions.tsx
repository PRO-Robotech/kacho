// registerExtensions — регистрация доменных расширений карточки compute-remote.
// Импортируется как side-effect входной точкой модуля (InstancesPage), поэтому
// расширения подключены на старте бандла, до рендера страниц.
//
// Оболочка карточки (`ResourceShell`) остаётся общей и app-agnostic: доменное
// содержимое машины — строки Обзора (вид, размещение, размер, источник ОС,
// статус, FQDN), действия шапки (пуск/остановка/перезапуск) и вкладки «Диски» /
// «Сетевые интерфейсы» — инжектится ЗДЕСЬ через `registerDetailExtension`.
//
// Здесь стоял ВТОРОЙ реестр расширений — собственная копия
// `ResourceDetailExtensions` этого модуля (185 строк): свои объявления типов
// `DescItem`/`DetailExtCtx`/`DetailExtension`, свой `detailExtension` и свои
// помощники показа значения (`txt`/`code` поверх `String(v)`). Общий реестр
// параметр для доменного содержимого уже имел, поэтому копия ничего не давала и
// только отставала: `String(v)` вместо `displayText(v)` показывает `[object
// Object]` на вложенном поле, а `Typography.Text code` — второй вид того же
// моноширинного значения, которое в строке свойств рисует `MonoValue`.

import { registerDetailExtension } from "@shared/components/organisms/ResourceDetailExtensions";
import type { DetailTab } from "@shared/components/organisms/DetailShell";
import { MonoValue } from "@shared/components/atoms/CopyableId/MonoValue";
import { StatusBadge } from "@shared/components/atoms/StatusBadge";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
// Ссылка на ресурс IAM — своя, потому что его адрес не project-scoped
// (`/iam/<route>/<id>`, см. @shared/lib/service-prefix).
import { IamRefLink } from "@shared/components/molecules/IamRefLink";
import { getByPath } from "@shared/lib/resource-registry";
import { displayText } from "@shared/lib/display-text";
import { formatBytes } from "@shared/lib/bytes";

import { InstanceActions } from "@/components/organisms/instance/InstanceActions";
import { InstanceDisksTab } from "@/components/organisms/instance/InstanceDisksTab";
import { InstanceNicsTab } from "@/components/organisms/instance/InstanceNicsTab";

/** Значение строки свойств словами; пусто — прочерк, как у «Описания» Обзора. */
function text(v: unknown): string {
  return displayText(v) || "—";
}

// effective_resources.memory_mib приходит в МиБ (int64 строкой); общий
// `formatBytes` считает байты, поэтому величина переводится здесь. Пусто или не
// число → NaN, и `formatBytes` отвечает прочерком, а не «0 B».
function mibToBytes(v: unknown): number {
  const mib = typeof v === "string" ? Number.parseInt(v, 10) : typeof v === "number" ? v : Number.NaN;
  return Number.isFinite(mib) && mib > 0 ? mib * 1024 * 1024 : Number.NaN;
}

function diskCount(data: Record<string, unknown>): number {
  const boot = getByPath<Record<string, unknown>>(data, "boot_disk");
  const secondary = getByPath<unknown[]>(data, "secondary_disks") ?? [];
  return (boot && (boot.volume_id || boot.device_name) ? 1 : 0) + secondary.length;
}

registerDetailExtension("compute-instances", {
  overviewExtra: ({ data }) => {
    const memBytes = mibToBytes(getByPath<unknown>(data, "effective_resources.memory_mib"));
    const bootType = getByPath<string>(data, "boot_source.type");
    const bootId = getByPath<string>(data, "boot_source.id");
    const bootName = getByPath<string>(data, "boot_source.name");
    const bootDigest = getByPath<string>(data, "boot_source.resolved_digest") ?? "";
    const bootVolume = getByPath<string>(data, "boot_source.materialized_volume.volume_id");
    const statusReason = getByPath<string>(data, "status_reason");
    const fqdn = getByPath<string>(data, "fqdn") ?? "";
    return [
      { label: "Тип инстанса", value: <MonoValue value={getByPath<string>(data, "instance_kind") ?? ""} /> },
      // Зона — ресурс каталога размещения со своей карточкой: ссылка, а не
      // строка. Ссылка на чужой ресурс в консоли ровно одна (`RefNameLink` →
      // `ResourceLink`), и своего значка копирования внутри строки свойств она
      // не рисует — там кнопка одна на строку и стоит справа столбцом.
      { label: "Зона доступности", value: <RefNameLink specId="zones" refId={getByPath<string>(data, "zone_id")} /> },
      // Тип машины — навигируемый каталог со своей карточкой (маршрут
      // `machine-types` этого же модуля), поэтому идентификатор показан
      // ссылкой, а не моноширинным текстом: в этом же перечне зона и
      // загрузочный том ссылками уже были, и два вида одного предмета читались
      // как два разных предмета.
      {
        label: "Тип машины",
        value: <RefNameLink specId="machine-types" refId={getByPath<string>(data, "machine_type_id")} />,
      },
      { label: "vCPU", value: text(getByPath<unknown>(data, "effective_resources.v_cpu")) },
      { label: "Память", value: formatBytes(memBytes) },
      { label: "Гарантия CPU, %", value: text(getByPath<unknown>(data, "cpu_guarantee_percent")) },
      { label: "Источник ОС", value: <MonoValue value={bootType ?? ""} /> },
      // Образ ссылкой — ТОЛЬКО когда он и вправду ресурс storage: у контейнера
      // источник приезжает из реестра образов координатой вида
      // `<реестр>/<репозиторий>:<тег>`, и карточки в этом разделе у неё нет.
      // Ссылка, ведущая в никуда, обещает переход, которого нет.
      {
        label: "Образ",
        value:
          bootType === "storage.image" && bootId ? (
            <RefNameLink specId="images" refId={bootId} />
          ) : bootName ? (
            text(bootName)
          ) : (
            <MonoValue value={bootId ?? ""} />
          ),
      },
      { label: "Дайджест образа", value: <MonoValue value={bootDigest} />, copy: bootDigest || undefined },
      // Загрузочный том — ресурс storage со своей карточкой.
      { label: "Загрузочный том", value: <RefNameLink specId="volumes" refId={bootVolume} /> },
      // Служебная учётка — ресурс IAM со своей карточкой; её адрес не
      // project-scoped, поэтому ссылку строит IamRefLink, а не RefNameLink.
      {
        label: "Сервисный аккаунт",
        value: <IamRefLink specId="service-accounts" refId={getByPath<string>(data, "service_account.id")} />,
      },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
      ...(statusReason ? [{ label: "Причина статуса", value: text(statusReason) }] : []),
      // FQDN переносят в чужое поле (запись DNS, конфигурация клиента) — значит
      // у строки объявляется, ЧТО копировать: общий значок строки свойств
      // копирует ИСХОДНОЕ значение, а не показанное.
      { label: "FQDN", value: <MonoValue value={fqdn} />, copy: fqdn || undefined },
    ];
  },
  headerActions: ({ data, projectId }) => (
    <InstanceActions
      instanceId={getByPath<string>(data, "id") ?? ""}
      status={getByPath<string>(data, "status")}
      projectId={projectId}
    />
  ),
  extraTabs: ({ data, projectId }): DetailTab[] => {
    const instanceId = getByPath<string>(data, "id") ?? "";
    const nics = getByPath<unknown[]>(data, "network_interfaces") ?? [];
    return [
      {
        id: "disks",
        label: "Диски",
        count: diskCount(data),
        render: () => <InstanceDisksTab instanceId={instanceId} projectId={projectId} data={data} />,
      },
      {
        id: "nics",
        label: "Сетевые интерфейсы",
        count: nics.length,
        render: () => <InstanceNicsTab instanceId={instanceId} projectId={projectId} data={data} />,
      },
    ];
  },
});
