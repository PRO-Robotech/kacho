// ArtifactTypeTag — иконка типа OCI-артефакта репозитория (docker-образ /
// helm-чарт / иной). Значение — enum-имя из REST-проекции (ARTIFACT_TYPE_*,
// сериализуется gateway'ем как строка). UNSPECIFIED / пусто → «—». Тип
// показываем компактной цветной иконкой (текстовую метку — в tooltip/aria).
//
// Репозиторий может быть СМЕШАННЫМ (docker-образы + helm-чарты одновременно):
// проекция несёт массив artifact_types. ArtifactTypesTag рендерит по иконке на
// каждый тип; ArtifactTypeTag — одиночное значение (совместимость сохранена).

import { type FC, type ComponentType } from "react";
import { Tooltip, Typography } from "antd";
import { Container, ShipWheel, Package, type LucideProps } from "lucide-react";

// Метаданные отображения per enum-значение: иконка (lucide), цвет, метка.
const ARTIFACT_META: Record<string, { label: string; color: string; Icon: ComponentType<LucideProps> }> = {
  // Container — контейнерный образ (docker/OCI); ShipWheel («штурвал») — Helm-чарт.
  ARTIFACT_TYPE_CONTAINER_IMAGE: { label: "Docker-образ", color: "#2496ed", Icon: Container },
  ARTIFACT_TYPE_HELM_CHART: { label: "Helm-чарт", color: "#6e56cf", Icon: ShipWheel },
  ARTIFACT_TYPE_OTHER: { label: "Иной", color: "var(--kc-text-tertiary, #8b929e)", Icon: Package },
};

/** artifactTypeLabel — человекочитаемая метка типа (для фильтра/тестов/tooltip). */
export function artifactTypeLabel(value: unknown): string {
  const key = typeof value === "string" ? value : "";
  return ARTIFACT_META[key]?.label ?? "—";
}

/** artifactTypeLabels — метки для набора типов (scalar ИЛИ array), только
 *  распознанные, без дублей; порядок — как во входе. Для tooltip/тестов. */
export function artifactTypeLabels(value: unknown): string[] {
  const values = Array.isArray(value) ? value : value == null ? [] : [value];
  const seen = new Set<string>();
  const out: string[] = [];
  for (const v of values) {
    const meta = ARTIFACT_META[typeof v === "string" ? v : ""];
    if (meta && !seen.has(meta.label)) {
      seen.add(meta.label);
      out.push(meta.label);
    }
  }
  return out;
}

// ArtifactIcon — одна цветная иконка типа с tooltip/aria-меткой.
const ArtifactIcon: FC<{ meta: { label: string; color: string; Icon: ComponentType<LucideProps> } }> = ({ meta }) => {
  const { Icon, label, color } = meta;
  return (
    <Tooltip title={label}>
      <span aria-label={label} role="img" style={{ display: "inline-flex", alignItems: "center", color }}>
        <Icon size={19} strokeWidth={1.75} />
      </span>
    </Tooltip>
  );
};

export const ArtifactTypeTag: FC<{ value: unknown }> = ({ value }) => {
  const key = typeof value === "string" ? value : "";
  const meta = ARTIFACT_META[key];
  if (!meta) {
    // UNSPECIFIED / отсутствует — тип не определён (best-effort classify).
    return <Typography.Text type="secondary">—</Typography.Text>;
  }
  return <ArtifactIcon meta={meta} />;
};

// ArtifactTypesTag — тип(ы) репозитория: одна иконка на каждый распознанный тип
// (смешанный репозиторий → docker + helm рядом). value — enum-имя ИЛИ массив имён.
// Пусто / только UNSPECIFIED → «—». Дубли схлопываются.
export const ArtifactTypesTag: FC<{ value: unknown }> = ({ value }) => {
  const values = Array.isArray(value) ? value : value == null ? [] : [value];
  const seen = new Set<string>();
  const metas: { label: string; color: string; Icon: ComponentType<LucideProps> }[] = [];
  for (const v of values) {
    const meta = ARTIFACT_META[typeof v === "string" ? v : ""];
    if (meta && !seen.has(meta.label)) {
      seen.add(meta.label);
      metas.push(meta);
    }
  }
  if (metas.length === 0) {
    return <Typography.Text type="secondary">—</Typography.Text>;
  }
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      {metas.map((meta) => (
        <ArtifactIcon key={meta.label} meta={meta} />
      ))}
    </span>
  );
};
