// SubnetCidrChips — controlled-вариант SubnetCidrManager для контекстов, где
// subnet ещё не существует (Create-форма). Визуально идентичен Edit-виджету
// (карточка с заголовком + chip-list + input + Add), но мутирует локальный
// state через onChange, а не вызывает :add-cidr-blocks / :remove-cidr-blocks
// verbs. Используется в InlineSubnetCreateForm.
//
// Edit-mode (subnet уже существует) → SubnetCidrManager (API).
// Create-mode (subnet ещё нет) → SubnetCidrChips (controlled).

import { useState } from "react";
import { Button, Card, Input, Space, Tag, Typography } from "antd";
import { CloseOutlined, PlusOutlined } from "@ant-design/icons";
import { toast } from "@shared/lib/toast";

type CidrKind = "v4" | "v6";

function validateCidr(kind: CidrKind, cidr: string): string | null {
  if (!cidr) return "Введите CIDR.";
  if (!cidr.includes("/")) return "CIDR должен содержать префикс (например /24).";
  if (kind === "v6" && !cidr.includes(":")) return "Похоже не на IPv6-адрес.";
  return null;
}

interface SectionProps {
  kind: CidrKind;
  blocks: string[];
  onChange: (next: string[]) => void;
  /** Скрыть заголовок карточки (когда identity даёт Form.Item-label слева). */
  hideTitle?: boolean;
}

export function CidrSection({ kind, blocks, onChange, hideTitle }: SectionProps) {
  const [draft, setDraft] = useState("");
  const label = kind === "v4" ? "IPv4 CIDR blocks" : "IPv6 CIDR blocks";
  const placeholder = kind === "v4" ? "10.0.1.0/24" : "fd00:1234::/64";
  const tagColor = kind === "v4" ? "blue" : "geekblue";

  const onAdd = () => {
    const cidr = draft.trim();
    const err = validateCidr(kind, cidr);
    if (err) {
      toast.error(err);
      return;
    }
    if (blocks.includes(cidr)) {
      toast.error("Этот CIDR уже добавлен.");
      return;
    }
    onChange([...blocks, cidr]);
    setDraft("");
  };

  const onRemove = (cidr: string) => {
    onChange(blocks.filter((c) => c !== cidr));
  };

  // AntD Card с size="small" + theme tokens (Modal-внутренний фон) — visual
  // parity с остальной формой; вместо «убогих» Tailwind-чипов теперь
  // полноценные AntD Tag'и с встроенной кнопкой закрытия.
  return (
    <Card
      size="small"
      title={
        hideTitle ? undefined : (
          <Space size={8}>
            <Typography.Text strong>{label}</Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>
              {blocks.length} блок(ов)
            </Typography.Text>
          </Space>
        )
      }
    >
      <Space direction="vertical" size={8} style={{ width: "100%" }}>
        <div style={{ minHeight: 24 }}>
          {blocks.length === 0 ? (
            <Typography.Text type="secondary" italic style={{ fontSize: 12 }}>
              — пусто —
            </Typography.Text>
          ) : (
            <Space size={[6, 6]} wrap>
              {blocks.map((cidr) => (
                <Tag
                  key={cidr}
                  color={tagColor}
                  closable
                  closeIcon={<CloseOutlined style={{ fontSize: 10 }} />}
                  onClose={(e) => {
                    e.preventDefault();
                    onRemove(cidr);
                  }}
                  style={{ fontFamily: "monospace", fontSize: 12, margin: 0 }}
                >
                  {cidr}
                </Tag>
              ))}
            </Space>
          )}
        </div>
        <Space.Compact style={{ width: "100%" }}>
          <Input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={placeholder}
            style={{ fontFamily: "monospace", fontSize: 12 }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                onAdd();
              }
            }}
          />
          <Button type="primary" ghost onClick={onAdd} disabled={!draft.trim()} icon={<PlusOutlined />}>
            Add
          </Button>
        </Space.Compact>
      </Space>
    </Card>
  );
}

interface Props {
  v4Blocks: string[];
  onV4Change: (next: string[]) => void;
  v6Blocks: string[];
  onV6Change: (next: string[]) => void;
}

export function SubnetCidrChips({ v4Blocks, onV4Change, v6Blocks, onV6Change }: Props) {
  return (
    <div className="space-y-3">
      <CidrSection kind="v4" blocks={v4Blocks} onChange={onV4Change} />
      <CidrSection kind="v6" blocks={v6Blocks} onChange={onV6Change} />
    </div>
  );
}
