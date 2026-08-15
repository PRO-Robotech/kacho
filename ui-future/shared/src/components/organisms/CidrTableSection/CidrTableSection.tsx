// CidrTableSection — секция CIDR-блоков одного семейства (v4/v6): шапка с
// бейджем семейства, таблица [CIDR-блок | ⌫] и строка ввода в подвале.
//
// Единственный вид для ВСЕХ наборов CIDR консоли — блоки подсети и объявленный
// супернет сети. Прежде их рисовали два разных компонента: подсеть — таблицей,
// сеть — карточкой с чипами, — при одинаковой механике (add и remove суть
// РАЗНЫЕ глаголы края, каждое действие уходит сразу своим RPC, без batch-save).
// Два вида одного предмета читались как два разных предмета.
//
// Край запрещает менять эти блоки через PATCH (immutable after Create) — только
// глаголы `:add-cidr-blocks` / `:remove-cidr-blocks`, отвечающие Operation.
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button, Input, Space, Spin, Tag, Tooltip, Typography } from "antd";
import { DeleteOutlined, LoadingOutlined, LockOutlined, PlusOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { OperationToastWatcher } from "@shared/components/molecules/OperationToastWatcher";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { SectionHeader } from "@shared/components/molecules/SectionHeader";
import { toast } from "@shared/lib/toast";

export type CidrKind = "v4" | "v6";

// Имена полей семейства задаёт ВЛАДЕЛЕЦ ресурса, а не эта секция.
//
// Здесь стояла общая пара `ipv4_cidr_blocks`/`ipv6_cidr_blocks` — верная для
// подсети и сети и НЕВЕРНАЯ для набора префиксов, чьи глаголы принимают
// `v4_cidr_blocks`/`v6_cidr_blocks`. Умолчание в таком месте — худший из
// исходов: край разбирает тело, отбрасывая неизвестные ключи МОЛЧА, поэтому
// действие вернуло бы успех, ничего не изменив. Поэтому пара обязательна и
// объявляется у ресурса, рядом с его же путём глагола.
export type CidrBlockFields = Record<CidrKind, string>;

/** Пара имён для ресурсов, чьи глаголы ключуют семейство полным словом. */
export const IP_PREFIXED_BLOCK_FIELDS: CidrBlockFields = {
  v4: "ipv4_cidr_blocks",
  v6: "ipv6_cidr_blocks",
};

const MONO_FONT = "ui-monospace, monospace";
const ROW_H = 41;

export function validateCidr(kind: CidrKind, cidr: string, prefixExample: string): string | null {
  if (!cidr) return "Введите CIDR.";
  if (!cidr.includes("/")) return `CIDR должен содержать префикс (например ${prefixExample}).`;
  if (kind === "v6" && !cidr.includes(":")) return "Похоже не на IPv6-адрес.";
  return null;
}

// Бейдж семейства в плитке шапки — «IPv4» / «IPv6» (mono, мелко чтобы влезло).
const familyTile = (text: string) => (
  <span style={{ fontSize: 10.5, fontWeight: 700, fontFamily: MONO_FONT, letterSpacing: "-0.04em" }}>{text}</span>
);

export interface CidrTableSectionProps {
  /** Путь глагола края. Строит его ВЛАДЕЛЕЦ ресурса, а не эта секция: голова
   *  пути обязана оставаться статически видимой пробе поверхности API
   *  (`lib/api-path-surface`), которая резолвит её в константу файла. Спрятав
   *  путь за проп, секция вывела бы оба ресурса из-под её наблюдения. */
  actionPath: (verb: "add" | "remove") => string;
  /** Как ВЛАДЕЛЕЦ называет поля семейства в теле глагола. Обязателен: умолчание
   *  здесь дало бы успешный ответ на действие, ничего не изменившее. */
  blockFields: CidrBlockFields;
  /** Префикс ключа кэша владельца: обновляется и карточка, и список. */
  invalidateKey: string;
  kind: CidrKind;
  /** Изменяемые блоки (уходят через глаголы). */
  blocks: string[];
  /** Заголовок секции: «CIDR» у подсети, «Супернет» у сети. */
  title: string;
  /** Пример префикса в тексте отказа валидации: «/24» у подсети, «/16» у сети. */
  prefixExample: string;
  placeholder: string;
  /** Как назван блок в тексте операции: «CIDR» / «супернет-блока». */
  opNoun: string;
  /** Как назван набор в тексте отказа: «CIDR» / «супернет». */
  errNoun: string;
  emptyText: string;
  /** Неизменяемый основной блок — показывается запертым, глаголами не проходит. */
  primary?: string;
  /** Подпись у замка основного блока. */
  primaryHint?: string;
}

export function CidrTableSection({
  actionPath,
  blockFields,
  invalidateKey,
  kind,
  blocks,
  title,
  prefixExample,
  placeholder,
  opNoun,
  errNoun,
  emptyText,
  primary,
  primaryHint,
}: CidrTableSectionProps) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("");
  const [pendingCidr, setPendingCidr] = useState<string | null>(null);

  const family = kind === "v4" ? "IPv4" : "IPv6";
  const field = blockFields[kind];

  const mutate = useMutation({
    mutationFn: (params: { verb: "add" | "remove"; cidr: string }) =>
      api.action(actionPath(params.verb), { [field]: [params.cidr] }),
    onSuccess: (resp, vars) => {
      const id = extractOperationId(resp);
      if (id) {
        setOpTitle(`${vars.verb === "add" ? "Добавление" : "Удаление"} ${family} ${opNoun} ${vars.cidr}`);
        setOpId(id);
        setPendingCidr(vars.cidr);
      } else {
        // Широкий prefix-инвалидейт: матчит и карточку (shell-detail), и список
        // — узкие ключи не совпадали с ключом ResourceShell, и карточка
        // показывала прежний набор.
        void qc.invalidateQueries({ queryKey: [invalidateKey] });
        setPendingCidr(null);
      }
    },
    onError: (err, vars) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : err.message;
      toast.error(`${family} ${errNoun} ${vars.verb === "add" ? "добавление" : "удаление"}: ${m}`);
      setPendingCidr(null);
    },
  });

  const inputDisabled = mutate.isPending || opId !== null;

  const onAdd = () => {
    const cidr = draft.trim();
    const verr = validateCidr(kind, cidr, prefixExample);
    if (verr) {
      toast.error(verr);
      return;
    }
    if (blocks.includes(cidr)) {
      toast.error("Этот CIDR уже добавлен.");
      return;
    }
    setPendingCidr(cidr);
    mutate.mutate({ verb: "add", cidr });
    setDraft("");
  };

  const onRemove = (cidr: string) => {
    if (inputDisabled) return;
    setPendingCidr(cidr);
    mutate.mutate({ verb: "remove", cidr });
  };

  return (
    <div style={{ marginTop: 24, maxWidth: 760 }}>
      <SectionHeader
        icon={familyTile(family)}
        eyebrow="Список"
        title={
          <span>
            {title} <Typography.Text type="secondary">({(primary ? 1 : 0) + blocks.length})</Typography.Text>
          </span>
        }
      />

      <div
        style={{
          border: "1px solid var(--kc-border)",
          borderRadius: 8,
          overflow: "hidden",
          background: "var(--kc-page)",
        }}
      >
        <table className="w-full text-sm kc-grid-table" style={{ tableLayout: "fixed" }}>
          <colgroup>
            <col style={{ width: "calc(100% - 48px)" }} />
            <col style={{ width: 48 }} />
          </colgroup>
          <thead>
            <tr style={{ background: "var(--kc-container)" }}>
              <th
                className="text-left"
                style={{
                  padding: "7px 12px",
                  fontSize: 11,
                  fontWeight: 600,
                  letterSpacing: "0.02em",
                  color: "var(--kc-text-tertiary)",
                }}
              >
                CIDR-блок
              </th>
              <th style={{ padding: "7px 4px" }} />
            </tr>
          </thead>
          <tbody>
            {!primary && blocks.length === 0 && (
              <tr style={{ height: ROW_H, borderTop: "1px solid var(--kc-border-secondary)" }}>
                <td
                  colSpan={2}
                  style={{
                    textAlign: "center",
                    verticalAlign: "middle",
                    fontSize: 12,
                    color: "var(--kc-text-tertiary)",
                  }}
                >
                  {emptyText}
                </td>
              </tr>
            )}
            {primary && (
              <tr className="kc-kv-row" style={{ height: ROW_H, borderTop: "1px solid var(--kc-border-secondary)" }}>
                <td className="px-3 font-mono text-xs" style={{ verticalAlign: "middle" }}>
                  {primary}{" "}
                  <Tag color="default" style={{ marginLeft: 6, fontFamily: MONO_FONT }}>
                    основной
                  </Tag>
                </td>
                <td className="px-1 text-center" style={{ verticalAlign: "middle" }}>
                  <Tooltip title={primaryHint}>
                    <LockOutlined style={{ color: "var(--kc-text-tertiary)", fontSize: 12 }} />
                  </Tooltip>
                </td>
              </tr>
            )}
            {blocks.map((cidr, i) => {
              const busy = pendingCidr === cidr && (mutate.isPending || opId !== null);
              return (
                <tr
                  key={i}
                  className="kc-kv-row"
                  style={{ height: ROW_H, borderTop: "1px solid var(--kc-border-secondary)" }}
                >
                  <td className="px-3 font-mono text-xs" style={{ verticalAlign: "middle" }}>
                    {cidr}
                  </td>
                  <td className="px-1 text-center" style={{ verticalAlign: "middle" }}>
                    {busy ? (
                      <Spin indicator={<LoadingOutlined style={{ fontSize: 12 }} spin />} />
                    ) : (
                      <Button
                        type="text"
                        danger
                        size="small"
                        icon={<DeleteOutlined />}
                        aria-label="Удалить CIDR"
                        onClick={() => onRemove(cidr)}
                        disabled={inputDisabled}
                      />
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
          <tfoot>
            <tr style={{ borderTop: "1px solid var(--kc-border-secondary)" }}>
              <td style={{ padding: "8px 10px" }} colSpan={2}>
                <Space.Compact style={{ width: "100%" }}>
                  <Input
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    placeholder={placeholder}
                    disabled={inputDisabled}
                    style={{ fontFamily: MONO_FONT, fontSize: 12.5 }}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.preventDefault();
                        onAdd();
                      }
                    }}
                  />
                  <Button
                    type="dashed"
                    icon={<PlusOutlined />}
                    onClick={onAdd}
                    disabled={!draft.trim() || inputDisabled}
                  >
                    Добавить
                  </Button>
                </Space.Compact>
              </td>
            </tr>
          </tfoot>
        </table>
      </div>

      <OperationToastWatcher
        opId={opId}
        title={opTitle}
        onDone={() => {
          setOpId(null);
          setPendingCidr(null);
          void qc.invalidateQueries({ queryKey: [invalidateKey] });
        }}
      />
    </div>
  );
}
