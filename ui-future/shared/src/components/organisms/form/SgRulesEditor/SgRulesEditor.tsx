// SgRulesEditor — редактор VPC SecurityGroupRule[]. Компактный AntD-Collapse:
// каждое правило свёрнуто в одну строку summary («↓ INGRESS · TCP · 80–80 · CIDR …»);
// expand → форма редактирования.
//
// Proto shape (kacho/cloud/vpc/v1/security_group.proto::SecurityGroupRule):
//   - direction: INGRESS | EGRESS  (required)
//   - description, labels
//   - protocol_name | protocol_number  (либо name, либо number, либо ничего = any)
//   - ports: PortRange { from_port, to_port }  (отсутствие = any)
//   - oneof target { cidr_blocks | security_group_id | cidr_group_id } — РОВНО ОДНА цель
//
// Третья ветвь — ссылка на ИМЕНОВАННЫЙ набор префиксов (`CidrGroup`). Она
// существует ради того, чтобы список, повторяющийся во многих правилах, перестал
// в них копироваться: правило называет набор, набор правится один раз. Пока
// редактор знал две ветви из трёх, эта возможность была объявлена контрактом и
// невыразима из консоли.
//
// Предустановленная цель СНЯТА с контракта (номер и имя зарезервированы, разрыв
// объявлен в перечне `proto/declared-breaks.yaml`). Она была принимаемым, но
// нечитаемым полем: край её сохранял, а правило не разрешало ничего. Держать её
// выбором в форме значило бы предлагать арендатору исход, которого нет.
//
// «Ровно одна» — не пожелание формы, а проверка сервиса: правило без цели и
// правило с двумя целями отвергаются с указанием поля `<путь>.target`. Поэтому
// набор целей здесь СУЖЕН ТИПОМ, а не только скрыт из списка: непредставимый
// выбор нельзя отправить, тогда как скрытый — можно, придя из уже сохранённого
// правила.

import { useState } from "react";
import {
  Button as AntButton,
  Card,
  Checkbox,
  Collapse,
  Form,
  Input as AntInput,
  InputNumber,
  Select as AntSelect,
  Space,
  Tag,
  Typography,
} from "antd";
import { CloseOutlined, DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { RefSelect } from "@shared/components/organisms/form/RefSelect";
import { getByPath, setByPath, deleteByPath } from "@shared/lib/path";
import { hasProtocolNumber } from "@shared/lib/resource-registry";

type ProtocolMode = "any" | "name" | "number";
type TargetKind = "cidr" | "sg" | "cidr-group";

export interface RuleExt {
  direction?: string;
  description?: string;
  /**
   * Метки правила. Редактор их не показывает и не правит, но НАЗЫВАЕТ и
   * проносит: правило пересоздаётся из черновика целиком (правка — это
   * `deletion_rule_ids` + `addition_rule_specs`), поэтому поле, которого
   * черновик не назвал, у правила исчезает. Прежде поле выживало ТОЛЬКО
   * распространением объекта в `{ ...rule }` — то есть случайно, а не
   * утверждением: первая же пересборка правила перечислением полей унесла бы
   * его молча. Состав против контракта держит
   * `test/set-replacement-draft-composition`.
   */
  labels?: Record<string, string>;
  _protocol_mode?: ProtocolMode;
  protocol_name?: string;
  // int64 on the wire → a JSON string when it comes from the server.
  protocol_number?: number | string;
  _ports_any?: boolean;
  ports?: { from_port?: number; to_port?: number };
  _target_kind?: TargetKind;
  cidr_blocks?: { v4_cidr_blocks?: string[]; v6_cidr_blocks?: string[] };
  security_group_id?: string;
  cidr_group_id?: string;
  id?: string;
}

interface Props {
  pathPrefix: string;
  value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  path: string;
  description?: string;
  // KAC-243 (scenario 18): network_id редактируемой SG. SG-target rule может
  // ссылаться только на SG из ТОЙ ЖЕ сети (SG на разных сетях физически
  // изолированы). Picker target-SG фильтрует список по
  // networkId === editingNetworkId. Если не задан (нет контекста сети) —
  // picker деградирует до ручного ввода UUID.
  editingNetworkId?: string;
}

function inferProtocolMode(r: RuleExt): ProtocolMode {
  if (r._protocol_mode) return r._protocol_mode;
  if (r.protocol_name) return "name";
  if (hasProtocolNumber(r.protocol_number)) return "number";
  return "any";
}

function inferPortsAny(r: RuleExt): boolean {
  if (typeof r._ports_any === "boolean") return r._ports_any;
  return !r.ports || (r.ports.from_port == null && r.ports.to_port == null);
}

function inferTargetKind(r: RuleExt): TargetKind {
  if (r._target_kind) return r._target_kind;
  if (r.cidr_blocks) return "cidr";
  if (r.security_group_id) return "sg";
  // Правило приезжает с сервера без служебных ключей формы, поэтому ветвь
  // выводится по заполненной цели. Без этой строки правило, ссылающееся на
  // набор, открывалось бы как «CIDR-блоки», и первое же сохранение сменило бы
  // цель на другую — молча.
  if (r.cidr_group_id) return "cidr-group";
  return "cidr";
}

export function emptyRule(): RuleExt {
  return {
    direction: "INGRESS",
    description: "",
    _protocol_mode: "any",
    _ports_any: true,
    _target_kind: "cidr",
    cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] },
  };
}

// Пресеты — частые INGRESS-правила в один клик (быстрый старт, чище UX).
function ingressTcp(port: number): RuleExt {
  return {
    direction: "INGRESS",
    _protocol_mode: "name",
    protocol_name: "TCP",
    _ports_any: false,
    ports: { from_port: port, to_port: port },
    _target_kind: "cidr",
    cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] },
  };
}
const RULE_PRESETS: { label: string; make: () => RuleExt }[] = [
  { label: "SSH", make: () => ingressTcp(22) },
  { label: "HTTP", make: () => ingressTcp(80) },
  { label: "HTTPS", make: () => ingressTcp(443) },
  {
    label: "ICMP",
    make: () => ({
      direction: "INGRESS",
      _protocol_mode: "name",
      protocol_name: "ICMP",
      _ports_any: true,
      _target_kind: "cidr",
      cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] },
    }),
  },
];

// One-line сводка правила для свёрнутой Collapse-панели.
function ruleSummary(r: RuleExt): string {
  const parts: string[] = [];
  parts.push((r.direction ?? "INGRESS").toUpperCase());
  const proto = inferProtocolMode(r);
  if (proto === "any") parts.push("any");
  else if (proto === "name") parts.push((r.protocol_name || "?").toLowerCase());
  else parts.push(`proto ${r.protocol_number ?? "?"}`);
  if (!inferPortsAny(r)) {
    const f = r.ports?.from_port;
    const t = r.ports?.to_port;
    parts.push(f === t || t == null ? String(f ?? "?") : `${f}–${t}`);
  } else {
    parts.push("ports:any");
  }
  const tk = inferTargetKind(r);
  if (tk === "cidr") {
    const v4 = r.cidr_blocks?.v4_cidr_blocks ?? [];
    const v6 = r.cidr_blocks?.v6_cidr_blocks ?? [];
    const cs = [...v4, ...v6];
    parts.push(`CIDR ${cs[0] ?? "—"}${cs.length > 1 ? ` +${cs.length - 1}` : ""}`);
  } else if (tk === "cidr-group") {
    parts.push(`Набор ${r.cidr_group_id?.slice(0, 8) ?? "?"}`);
  } else {
    parts.push(`SG ${r.security_group_id?.slice(0, 8) ?? "?"}`);
  }
  return parts.join(" · ");
}

export function SgRulesEditor({ value, onChange, path, description, editingNetworkId }: Props) {
  const rules = (getByPath(value, path) as RuleExt[] | undefined) ?? [];
  // Активные (открытые) панели Collapse. По умолчанию все свёрнуты.
  // Новое правило открывается автоматически.
  const [activeKeys, setActiveKeys] = useState<string[]>([]);

  const setRule = (idx: number, next: RuleExt) => {
    const arr = [...rules];
    arr[idx] = next;
    onChange(setByPath(value, path, arr));
  };

  const addRule = () => {
    const nextIdx = rules.length;
    onChange(setByPath(value, path, [...rules, emptyRule()]));
    setActiveKeys((prev) => [...prev, String(nextIdx)]);
  };

  const addPreset = (make: () => RuleExt) => {
    onChange(setByPath(value, path, [...rules, make()]));
  };

  const ingressN = rules.filter((r) => (r.direction ?? "INGRESS") === "INGRESS").length;
  const egressN = rules.length - ingressN;

  const removeRule = (idx: number) => {
    onChange(deleteByPath(value, `${path}[${idx}]`));
    setActiveKeys((prev) => prev.filter((k) => k !== String(idx)));
  };

  return (
    <Card
      size="small"
      title={
        <Space size={10}>
          <Typography.Text strong>Правила</Typography.Text>
          {/* Живая сводка направлений (обновляется по мере добавления). */}
          <Tag color="green" style={{ margin: 0, fontSize: 11 }}>
            ↓ {ingressN} вход.
          </Tag>
          <Tag color="blue" style={{ margin: 0, fontSize: 11 }}>
            ↑ {egressN} исх.
          </Tag>
        </Space>
      }
      extra={
        <AntButton type="primary" ghost size="small" icon={<PlusOutlined />} onClick={addRule}>
          Добавить правило
        </AntButton>
      }
    >
      {description && (
        <Typography.Text type="secondary" style={{ fontSize: 11, display: "block", marginBottom: 8 }}>
          {description}
        </Typography.Text>
      )}
      {/* Быстрые пресеты — частые INGRESS-правила в один клик. */}
      <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", marginBottom: 12 }}>
        <Typography.Text type="secondary" style={{ fontSize: 11 }}>
          Быстро:
        </Typography.Text>
        {RULE_PRESETS.map((p) => (
          <AntButton key={p.label} size="small" icon={<PlusOutlined />} onClick={() => addPreset(p.make)}>
            {p.label}
          </AntButton>
        ))}
      </div>
      {rules.length === 0 ? (
        <Typography.Text type="secondary" italic style={{ fontSize: 12 }}>
          — пусто, трафик блокируется (default-deny) —
        </Typography.Text>
      ) : (
        <Collapse
          ghost
          activeKey={activeKeys}
          onChange={(k) => setActiveKeys(Array.isArray(k) ? k : [k])}
          items={rules.map((r, idx) => ({
            key: String(idx),
            label: (
              <Space size={6} style={{ width: "100%" }}>
                <Tag
                  color={(r.direction ?? "INGRESS") === "INGRESS" ? "green" : "blue"}
                  style={{ margin: 0, fontSize: 11 }}
                >
                  {(r.direction ?? "INGRESS").toUpperCase()}
                </Tag>
                <Typography.Text style={{ fontSize: 12 }}>
                  {ruleSummary({ ...r, direction: undefined })}
                </Typography.Text>
              </Space>
            ),
            extra: (
              <AntButton
                type="text"
                size="small"
                icon={<DeleteOutlined />}
                onClick={(e) => {
                  e.stopPropagation();
                  removeRule(idx);
                }}
                danger
              />
            ),
            children: <RuleBody rule={r} onChange={(next) => setRule(idx, next)} editingNetworkId={editingNetworkId} />,
          }))}
        />
      )}
    </Card>
  );
}

export function RuleBody({
  rule,
  onChange,
  editingNetworkId,
}: {
  rule: RuleExt;
  onChange: (next: RuleExt) => void;
  editingNetworkId?: string;
}) {
  const protoMode = inferProtocolMode(rule);
  const portsAny = inferPortsAny(rule);
  const targetKind = inferTargetKind(rule);

  const set = (patch: Partial<RuleExt>) => onChange({ ...rule, ...patch });

  return (
    <Form
      layout="horizontal"
      labelCol={{ flex: "200px" }}
      wrapperCol={{ flex: "1 1 0" }}
      labelAlign="left"
      colon={false}
      size="middle"
    >
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr",
          gap: 8,
        }}
      >
        <Field label="Направление" required>
          <AntSelect
            value={rule.direction ?? "INGRESS"}
            onChange={(v) => set({ direction: v })}
            options={[
              { value: "INGRESS", label: "INGRESS" },
              { value: "EGRESS", label: "EGRESS" },
            ]}
            style={{ width: "100%" }}
          />
        </Field>
        <Field label="Описание">
          <AntInput value={rule.description ?? ""} onChange={(e) => set({ description: e.target.value })} />
        </Field>
      </div>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr",
          gap: 8,
        }}
      >
        <Field label="Протокол">
          <AntSelect
            value={protoMode}
            onChange={(v: ProtocolMode) =>
              set({
                _protocol_mode: v,
                protocol_name: v === "name" ? (rule.protocol_name ?? "") : undefined,
                // Ветвь номера НЕ засевается значением: ноль сервер отвергает
                // («номер 0 неотличим от незаданного протокола»), а любой другой
                // засев выбрал бы протокол за оператора. Поле остаётся пустым,
                // и его заполняет тот, кто ветвь и выбрал (#375).
                protocol_number: v === "number" ? rule.protocol_number : undefined,
              })
            }
            options={[
              { value: "any", label: "Любой" },
              { value: "name", label: "По имени" },
              { value: "number", label: "По номеру" },
            ]}
            style={{ width: "100%" }}
          />
        </Field>
        {protoMode === "name" && (
          <Field label="Имя">
            <AntInput
              placeholder="tcp / udp / icmp / …"
              value={rule.protocol_name ?? ""}
              onChange={(e) => set({ protocol_name: e.target.value })}
            />
          </Field>
        )}
        {protoMode === "number" && (
          <Field label="Номер IANA">
            <InputNumber
              // Нижняя граница — ЕДИНИЦА, а не ноль: ноль сервер отвергает как
              // неотличимый от незаданного протокола, то есть значение, которое
              // поле предлагало по умолчанию, не принималось никогда.
              min={1}
              max={255}
              placeholder="1..255"
              value={rule.protocol_number ?? undefined}
              onChange={(v) => set({ protocol_number: v === null ? undefined : Number(v) })}
              style={{ width: "100%" }}
            />
          </Field>
        )}
        {protoMode === "any" && <div />}
      </div>

      <div>
        <Checkbox
          checked={portsAny}
          onChange={(e) =>
            set({
              _ports_any: e.target.checked,
              ports: e.target.checked ? undefined : { from_port: 0, to_port: 65535 },
            })
          }
        >
          Любые порты
        </Checkbox>
        {!portsAny && (
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "1fr 1fr",
              gap: 8,
              marginTop: 6,
            }}
          >
            <Field label="От">
              <InputNumber
                min={0}
                max={65535}
                value={rule.ports?.from_port ?? undefined}
                onChange={(v) =>
                  set({
                    ports: {
                      ...(rule.ports ?? {}),
                      from_port: v === null ? undefined : Number(v),
                    },
                  })
                }
                style={{ width: "100%" }}
              />
            </Field>
            <Field label="До">
              <InputNumber
                min={0}
                max={65535}
                value={rule.ports?.to_port ?? undefined}
                onChange={(v) =>
                  set({
                    ports: {
                      ...(rule.ports ?? {}),
                      to_port: v === null ? undefined : Number(v),
                    },
                  })
                }
                style={{ width: "100%" }}
              />
            </Field>
          </div>
        )}
      </div>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr",
          gap: 8,
        }}
      >
        <Field label="Источник">
          <AntSelect
            value={targetKind}
            onChange={(v: TargetKind) =>
              set({
                _target_kind: v,
                cidr_blocks: v === "cidr" ? (rule.cidr_blocks ?? { v4_cidr_blocks: ["0.0.0.0/0"] }) : undefined,
                security_group_id: v === "sg" ? (rule.security_group_id ?? "") : undefined,
                cidr_group_id: v === "cidr-group" ? (rule.cidr_group_id ?? "") : undefined,
              })
            }
            options={[
              { value: "cidr", label: "CIDR-блоки" },
              { value: "sg", label: "Группа безопасности" },
              { value: "cidr-group", label: "Набор префиксов" },
            ]}
            style={{ width: "100%" }}
          />
        </Field>
        <div>
          {targetKind === "cidr-group" && (
            // Набор — ресурс ТОГО ЖЕ проекта (край отвергает чужой), поэтому
            // выбор идёт списком проекта, а не свободной строкой: оператор не
            // обязан помнить идентификатор набора наизусть.
            <RefSelect
              refResource="cidr-groups"
              refProjectScoped
              value={rule.cidr_group_id ?? ""}
              onChange={(uid) => set({ cidr_group_id: uid })}
              placeholder="Выберите набор префиксов"
            />
          )}
          {targetKind === "sg" &&
            // KAC-243 (scenario 18): выбор target-SG только из ТОЙ ЖЕ сети.
            // refFilter оставляет лишь SG с networkId === editingNetworkId —
            // cross-network SG не selectable (физически изолированы). Если
            // network-контекст недоступен (editingNetworkId пуст) — fallback на
            // ручной ввод UUID, чтобы не блокировать редактор.
            (editingNetworkId ? (
              <RefSelect
                refResource="security-groups"
                refProjectScoped
                value={rule.security_group_id ?? ""}
                onChange={(uid) => set({ security_group_id: uid })}
                placeholder="Группа безопасности (та же сеть)"
                refFilter={(row) => row.network_id === editingNetworkId}
              />
            ) : (
              <AntInput
                placeholder="UUID другой SG"
                value={rule.security_group_id ?? ""}
                onChange={(e) => set({ security_group_id: e.target.value })}
              />
            ))}
        </div>
      </div>

      {targetKind === "cidr" && (
        <CidrEditor
          v4={rule.cidr_blocks?.v4_cidr_blocks ?? []}
          v6={rule.cidr_blocks?.v6_cidr_blocks ?? []}
          onChange={(v4, v6) =>
            set({
              cidr_blocks: {
                ...(v4.length > 0 ? { v4_cidr_blocks: v4 } : {}),
                ...(v6.length > 0 ? { v6_cidr_blocks: v6 } : {}),
              },
            })
          }
        />
      )}
    </Form>
  );
}

function CidrEditor({
  v4,
  v6,
  onChange,
}: {
  v4: string[];
  v6: string[];
  onChange: (v4: string[], v6: string[]) => void;
}) {
  return (
    <Space direction="vertical" size={8} style={{ width: "100%" }}>
      <CidrChipList
        label="IPv4 CIDR"
        placeholder="0.0.0.0/0"
        tagColor="blue"
        value={v4}
        onChange={(next) => onChange(next, v6)}
      />
      <CidrChipList
        label="IPv6 CIDR"
        placeholder="::/0"
        tagColor="geekblue"
        value={v6}
        onChange={(next) => onChange(v4, next)}
      />
    </Space>
  );
}

function CidrChipList({
  label,
  placeholder,
  tagColor,
  value,
  onChange,
}: {
  label: string;
  placeholder: string;
  tagColor: string;
  value: string[];
  onChange: (next: string[]) => void;
}) {
  const [draft, setDraft] = useState("");
  const add = () => {
    const v = draft.trim();
    if (!v) return;
    if (value.includes(v)) {
      setDraft("");
      return;
    }
    onChange([...value, v]);
    setDraft("");
  };
  return (
    <Card size="small" title={label} bordered>
      <Space direction="vertical" size={6} style={{ width: "100%" }}>
        <div style={{ minHeight: 22 }}>
          {value.length === 0 ? (
            <Typography.Text type="secondary" italic style={{ fontSize: 12 }}>
              — пусто —
            </Typography.Text>
          ) : (
            <Space size={[4, 4]} wrap>
              {value.map((cidr) => (
                <Tag
                  key={cidr}
                  color={tagColor}
                  closable
                  closeIcon={<CloseOutlined style={{ fontSize: 10 }} />}
                  onClose={(e) => {
                    e.preventDefault();
                    onChange(value.filter((c) => c !== cidr));
                  }}
                  style={{ fontFamily: "monospace", fontSize: 11, margin: 0 }}
                >
                  {cidr}
                </Tag>
              ))}
            </Space>
          )}
        </div>
        <Space.Compact style={{ width: "100%" }}>
          <AntInput
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={placeholder}
            style={{ fontFamily: "monospace", fontSize: 12 }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
          />
          <AntButton type="primary" ghost icon={<PlusOutlined />} disabled={!draft.trim()} onClick={add}>
            Добавить
          </AntButton>
        </Space.Compact>
      </Space>
    </Card>
  );
}

// Строка формы правила — тем же каноном, что и все формы консоли: подпись слева
// колонкой в 200px, поле справа. Прежде здесь была своя раскладка (подпись НАД
// полем, два поля в ряд), из-за чего форма правила выглядела чужой среди прочих.
function Field({ label, required, children }: { label: string; required?: boolean; children: React.ReactNode }) {
  return (
    <Form.Item label={label} required={required} style={{ marginBottom: 0 }}>
      {children}
    </Form.Item>
  );
}
