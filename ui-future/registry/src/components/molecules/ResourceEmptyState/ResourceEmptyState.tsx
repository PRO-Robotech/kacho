// ResourceEmptyState — продакшн-реди welcome для пустой таблицы ресурса.
// Единый для встроенных дочерних таблиц (ResourceShell/RelatedTable) и базовых
// list-страниц (ResourceListPage): иллюстрация (иконка ресурса) + заголовок +
// описание + крупный CTA + блок «Документация». Копирайт — из spec.emptyState
// (Kachō-style, без «yandex»), с generic-fallback.

import { Button, Typography } from "antd";
import { PlusOutlined, ReadOutlined, RightOutlined } from "@ant-design/icons";
import { ResourceIcon } from "@/components/organisms/form/ResourceIcon";
import type { ResourceSpec } from "@/lib/resource-registry";

interface Props {
  spec: ResourceSpec;
  onCreate: () => void;
  /** Переопределение текста кнопки (по умолчанию «Создать <singular>»). */
  createLabel?: string;
}

export function ResourceEmptyState({ spec, onCreate, createLabel }: Props) {
  const copy = spec.emptyState;
  const label = createLabel ?? `Создать ${spec.singular.toLowerCase()}`;
  return (
    <div
      style={{
        maxWidth: 600,
        margin: "0 auto",
        minHeight: "calc(100vh - 260px)",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        textAlign: "center",
        padding: "32px 16px",
      }}
    >
      <div
        style={{
          width: 96,
          height: 96,
          borderRadius: 24,
          marginBottom: 24,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          fontSize: 44,
          color: "#3D8DF5",
          background: "linear-gradient(135deg, rgba(61,141,245,0.16), rgba(61,141,245,0.04))",
          border: "1px solid var(--ant-color-border-secondary, #2f3138)",
          boxShadow: "0 1px 0 rgba(255,255,255,0.03) inset",
        }}
      >
        <ResourceIcon specId={spec.id} />
      </div>
      <Typography.Title level={4} style={{ margin: "0 0 10px", fontWeight: 600 }}>
        {copy?.title ?? `Создайте первый ресурс «${spec.singular.toLowerCase()}»`}
      </Typography.Title>
      {copy?.body && (
        <Typography.Paragraph
          type="secondary"
          style={{ fontSize: 14, lineHeight: 1.65, margin: "0 0 24px", maxWidth: 500 }}
        >
          {copy.body}
        </Typography.Paragraph>
      )}
      {/* Read-only ресурсы (repository материализуется через docker push) не
          показывают CTA «Создать» — кнопка гейтится по spec.ops.create. */}
      {spec.ops.create && (
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={onCreate}
          style={{ marginBottom: copy?.docs?.length ? 28 : 0 }}
        >
          {label}
        </Button>
      )}
      {copy?.docs && copy.docs.length > 0 && (
        <div
          style={{
            width: "100%",
            maxWidth: 460,
            textAlign: "left",
            padding: "14px 18px",
            background: "var(--ant-color-fill-quaternary, rgba(255,255,255,0.03))",
            border: "1px solid var(--ant-color-border-secondary, #2f3138)",
            borderRadius: 12,
          }}
        >
          <Typography.Text
            type="secondary"
            style={{ fontSize: 11, textTransform: "uppercase", letterSpacing: 0.5, fontWeight: 600 }}
          >
            <ReadOutlined style={{ marginRight: 6 }} />
            Документация
          </Typography.Text>
          <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 10 }}>
            {copy.docs.map((d) => (
              <Typography.Link
                key={d}
                href="#"
                style={{ fontSize: 13, display: "inline-flex", alignItems: "center", gap: 6 }}
              >
                <RightOutlined style={{ fontSize: 10, opacity: 0.6 }} />
                {d}
              </Typography.Link>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
