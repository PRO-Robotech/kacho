// referrer — чистые (без React/antd) хелперы для kacho.cloud.reference.Referrer.
// Вынесены из spec-columns.tsx, чтобы route/label-логику можно было unit-тестировать
// без тяжёлого import-графа рендер-слоя (RefNameLink → antd/@ant-design/icons).
// spec-columns.tsx ре-экспортирует referrerHref/referrerMeta для стабильности API.

// normalizeReferrerType — приводит referrer.type к единой underscore-форме.
// Сервисы отдают тип ссылки в ДВУХ формах: legacy underscore (`compute_instance`,
// vpc/compute/nlb) и canonical dotted `domain.resource` (`compute.instance`,
// storage-remote — kacho.cloud.reference.Referrer.type из shared-каталога). `.`→`_`
// делает обе формы одним ключом switch'ей ниже (на underscore-типе это no-op).
export function normalizeReferrerType(type: string | undefined): string {
  return (type ?? "").replace(/\./g, "_");
}

// referrerHref — маппинг kacho.cloud.reference.Reference.referrer → SPA-route.
// Строит host-level route НАПРЯМУЮ (без обращения к локальному registry remote'а),
// поэтому это ЕДИНСТВЕННЫЙ рабочий путь для CROSS-remote ссылки: storage-remote,
// рендеря Volume.used_by → `compute.instance`, не имеет compute-instances в СВОЁМ
// registry, значит REFERRER_SPEC→RefNameLink (name-резолв из локального registry)
// там не сработал бы — dotted-тип намеренно минует REFERRER_SPEC и линкуется здесь.
// Структурирован как switch по нормализованному типу — новый referrer-тип
// (compute_disk, nlb_target_group, ...) дописывается одним case'ом. Возвращает
// `null` если projectId не известен или тип не поддержан — caller рендерит
// plain-текст (forward-compat fallback).
export function referrerHref(
  projectId: string | null | undefined,
  referrer: { type?: string; id?: string } | undefined,
): string | null {
  if (!projectId) return null;
  const t = normalizeReferrerType(referrer?.type);
  const id = referrer?.id;
  if (!t || !id) return null;
  switch (t) {
    case "compute_instance":
      return `/projects/${projectId}/compute/instances/${id}`;
    default:
      return null;
  }
}

// referrerMeta — human-readable label + цвет текста для типа referrer'а. Известные
// типы получают короткие user-facing метки ("VM", "Disk", ...) и семантический цвет;
// unknown — fallback на сам `type` без цвета (neutral), чтобы forward-compat при
// появлении новых referrer-типов работал визуально. Цвета — hex'ы из стандартной
// палитры antd (https://ant.design/docs/spec/colors).
export function referrerMeta(type: string | undefined): { label: string; color?: string } {
  // Нормализуем dotted↔underscore, чтобы `compute.instance` и `compute_instance`
  // давали одну метку. default сохраняет ИСХОДНЫЙ тип (не нормализованный) для
  // читаемого forward-compat fallback у по-настоящему неизвестных типов.
  switch (normalizeReferrerType(type)) {
    case "compute_instance":
      return { label: "VM", color: "#1677ff" };
    case "compute_disk":
      return { label: "Disk", color: "#13c2c2" };
    case "compute_image":
      return { label: "Image", color: "#2f54eb" };
    case "compute_snapshot":
      return { label: "Snapshot", color: "#722ed1" };
    case "nlb_target_group":
      return { label: "NLB TG", color: "#faad14" };
    default:
      return { label: type || "?" };
  }
}
