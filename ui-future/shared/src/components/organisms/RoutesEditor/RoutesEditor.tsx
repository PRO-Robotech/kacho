// RoutesEditor — controlled-редактор статических маршрутов для ФОРМЫ создания
// таблицы маршрутизации. Вид — тот же, что у меток: таблица с ⌫ на строке и
// dashed-кнопкой «Добавить» снизу. Controlled: часть состояния формы, без
// собственного сохранения.
//
// ─────────────────────────────────────────────────────────────────────────────
// СЛЕДУЮЩИЙ УЗЕЛ — ВЗАИМОИСКЛЮЧАЮЩАЯ ГРУППА ИЗ ДВУХ ВЕТВЕЙ
//
// Контракт (`vpc/v1/route_table.proto`, `StaticRoute.next_hop`) несёт адрес ЛИБО
// шлюз. Здесь выражаются обе, и переключение ветви СНИМАЕТ прежнюю: строка,
// несущая обе, — отказ сервера (`exactly_one`).
//
// > [!note] Здесь стояло, что сервер ветвь шлюза не поддерживает — это было неверно
// > Прежняя редакция утверждала «Backend поддерживает только next_hop_address …
// > gateway_id не вводим», а второй такой же комментарий стоял в реестре ресурсов.
// > Оба описывали состояние, которого давно нет: домен маршрута хранит поле шлюза,
// > обработчик проверяет исключающее ИЛИ, и на эту ветвь есть собственные пробы
// > сервиса. Утверждение о ЧУЖОМ коде живёт ровно до тех пор, пока чужой код его
// > подтверждает; проверять это утверждение было некому, поэтому оно пережило свой
// > предмет и работало как ПРИЧИНА не делать — следующий читатель принимал его на
// > веру и не смотрел в дерево. Поэтому оговорка остаётся здесь вместо удаления:
// > объявляя чужое ограничение, называй, чем оно проверено.

import { Button, Input, Select } from "antd";
import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { RefSelect } from "@shared/components/organisms/form/RefSelect";

/**
 * Строка маршрута. Ветвь следующего узла представлена ровно одним из полей —
 * обоих сразу в строке не бывает по построению этого редактора.
 */
export interface RouteEntry {
  destination_prefix: string;
  next_hop_address?: string;
  gateway_id?: string;
  /**
   * Метки маршрута. Редактор их не показывает и не правит, но НАЗЫВАЕТ и
   * проносит: набор уезжает на край полной заменой, поэтому поле, которого нет
   * в черновике, стирается у всех строк разом. Прежде поле держалось тем, что
   * форма правки маршрутов скрыта (`editHidden`), — а скрытие не утверждение:
   * снявший флаг вернул бы #422 в полном виде и так же молча. Состав против
   * контракта держит `test/set-replacement-draft-composition`.
   */
  labels?: Record<string, string>;
}

type NextHopKind = "address" | "gateway";

/** Ветвь строки — по тому, какое поле в ней есть. */
function kindOf(r: RouteEntry): NextHopKind {
  return r.gateway_id !== undefined ? "gateway" : "address";
}

/**
 * Строка с ВЫБРАННОЙ ветвью следующего узла: прежняя ветвь снимается (строка,
 * несущая обе, — отказ сервера, `exactly_one`), всё остальное переносится
 * дословно.
 *
 * Прежде каждое такое место пересобирало строку ПЕРЕЧИСЛЕНИЕМ полей
 * (`{ destination_prefix, gateway_id }`), то есть стирало всё, чего редактор не
 * показывает. Пока в строке было три поля, стирать было нечего; с метками —
 * есть, и потеря была бы тихой. Снимается ровно одна ветвь, названная здесь, а
 * не всё, кроме перечисленного.
 */
function withNextHop(r: RouteEntry, next: { gateway_id: string } | { next_hop_address: string }): RouteEntry {
  const out: RouteEntry = { ...r, ...next };
  if ("gateway_id" in next) delete out.next_hop_address;
  else delete out.gateway_id;
  return out;
}

interface Props {
  value: RouteEntry[];
  onChange: (next: RouteEntry[]) => void;
  disabled?: boolean;
}

const ROW_H = 38;
const GRID = "minmax(0, 1.2fr) 150px minmax(0, 1.4fr) 40px";
const COL_DIVIDER = "1px solid var(--kc-border-secondary)";

const headCellStyle: React.CSSProperties = {
  padding: "7px 12px",
  fontSize: 11,
  fontWeight: 600,
  letterSpacing: "0.02em",
  color: "var(--kc-text-tertiary)",
  borderRight: COL_DIVIDER,
};

const cellWrapStyle: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  padding: "0 12px",
  minWidth: 0,
  borderRight: COL_DIVIDER,
};

const cellInputStyle: React.CSSProperties = {
  width: "100%",
  minWidth: 0,
  background: "transparent",
  fontFamily: "ui-monospace, monospace",
  fontSize: 12.5,
  padding: 0,
  height: ROW_H - 1,
  lineHeight: `${ROW_H - 1}px`,
};

export function RoutesEditor({ value, onChange, disabled }: Props) {
  const update = (idx: number, next: RouteEntry) => onChange(value.map((r, i) => (i === idx ? next : r)));

  /**
   * Смена ветви СНИМАЕТ прежнюю, а не дописывает поле рядом: иначе строка
   * унесла бы обе ветви и сервер отверг бы её целиком, а причина читалась бы как
   * «маршрут неверен», хотя неверен был редактор.
   */
  const switchKind = (idx: number, kind: NextHopKind) => {
    const r = value[idx];
    update(
      idx,
      kind === "gateway"
        ? withNextHop(r, { gateway_id: r.gateway_id ?? "" })
        : withNextHop(r, { next_hop_address: r.next_hop_address ?? "" }),
    );
  };

  return (
    <div
      style={{
        width: "100%",
        minWidth: 0,
        border: "1px solid var(--kc-border)",
        borderRadius: 8,
        overflow: "hidden",
        background: "var(--kc-page)",
      }}
    >
      <div style={{ display: "grid", gridTemplateColumns: GRID, background: "var(--kc-container)" }}>
        <div style={headCellStyle}>Префикс назначения</div>
        <div style={headCellStyle}>Вид</div>
        <div style={headCellStyle}>Следующий узел</div>
        <div />
      </div>

      {value.map((r, idx) => {
        const kind = kindOf(r);
        return (
          <div
            key={idx}
            className="kc-kv-row"
            style={{
              display: "grid",
              gridTemplateColumns: GRID,
              alignItems: "stretch",
              minWidth: 0,
              borderTop: "1px solid var(--kc-border-secondary)",
            }}
          >
            <div style={cellWrapStyle}>
              <Input
                variant="borderless"
                placeholder="10.0.0.0/24"
                value={r.destination_prefix}
                onChange={(e) => update(idx, { ...r, destination_prefix: e.target.value })}
                disabled={disabled}
                style={cellInputStyle}
              />
            </div>
            <div style={cellWrapStyle}>
              <Select
                aria-label="Вид следующего узла"
                variant="borderless"
                value={kind}
                onChange={(v) => switchKind(idx, v)}
                disabled={disabled}
                style={{ width: "100%" }}
                options={[
                  { value: "address", label: "Адрес" },
                  { value: "gateway", label: "Шлюз" },
                ]}
              />
            </div>
            <div style={cellWrapStyle}>
              {kind === "gateway" ? (
                <RefSelect
                  refResource="gateways"
                  refProjectScoped
                  value={r.gateway_id ?? ""}
                  onChange={(id) => update(idx, withNextHop(r, { gateway_id: id }))}
                  placeholder="Выберите шлюз"
                  disabled={disabled}
                />
              ) : (
                <Input
                  variant="borderless"
                  placeholder="10.0.0.1"
                  value={r.next_hop_address ?? ""}
                  onChange={(e) => update(idx, withNextHop(r, { next_hop_address: e.target.value }))}
                  disabled={disabled}
                  style={cellInputStyle}
                />
              )}
            </div>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
              <Button
                type="text"
                size="small"
                aria-label="Удалить маршрут"
                icon={<DeleteOutlined />}
                onClick={() => onChange(value.filter((_, i) => i !== idx))}
                disabled={disabled}
              />
            </div>
          </div>
        );
      })}

      <div style={{ borderTop: "1px solid var(--kc-border-secondary)", padding: 6 }}>
        <Button
          type="dashed"
          size="small"
          block
          icon={<PlusOutlined />}
          disabled={disabled}
          onClick={() => onChange([...value, { destination_prefix: "", next_hop_address: "" }])}
        >
          Добавить маршрут
        </Button>
      </div>
    </div>
  );
}
