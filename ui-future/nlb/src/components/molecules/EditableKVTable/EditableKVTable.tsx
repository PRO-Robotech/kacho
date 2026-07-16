// EditableKVTable — единый аккуратный controlled-редактор «таблица из двух
// колонок + ⌫ + dashed-кнопка добавления». Используется LabelsEditor (метки) и
// RoutesEditor (статические маршруты) → один вид, дедуп стилей.
//
// Стилистика на --kc-* (как остальной UI), а НЕ на shadcn-Tailwind-токенах:
//  • фон = var(--kc-page) — тот же, что у AntD-инпутов формы (виджет читается
//    как нативный контрол, «утопает» в карточке формы);
//  • шапка = var(--kc-container), мягкая, без uppercase tracking-wide;
//  • тонкие разделители var(--kc-border-secondary), hover var(--kc-hover-fill);
//  • borderless-инпуты в ячейках, моноширинный шрифт.
// div-grid (minmax(0,1fr)) — не <table>, чтобы виджет сжимался и не выталкивал
// layout формы (KAC-246 gotcha).
import { Button, Input } from "antd";
import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";

export interface KVRow {
  a: string;
  b: string;
}

interface ColDef {
  header: string;
  placeholder: string;
}

interface Props {
  rows: KVRow[];
  onChange: (rows: KVRow[]) => void;
  colA: ColDef;
  colB: ColDef;
  addLabel: string;
  disabled?: boolean;
}

const ROW_H = 38;
const GRID_COLS = "minmax(0, 1fr) minmax(0, 1fr) 40px";

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

export function EditableKVTable({ rows, onChange, colA, colB, addLabel, disabled }: Props) {
  const update = (idx: number, patch: Partial<KVRow>) => {
    onChange(rows.map((row, i) => (i === idx ? { ...row, ...patch } : row)));
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
      {/* header */}
      <div style={{ display: "grid", gridTemplateColumns: GRID_COLS, background: "var(--kc-container)" }}>
        <div style={headCellStyle}>{colA.header}</div>
        <div style={headCellStyle}>{colB.header}</div>
        <div />
      </div>

      {/* rows (пустое состояние не показываем — только кнопка «Добавить» в футере) */}
      {rows.map((r, idx) => (
        <div
          key={idx}
          className="kc-kv-row"
          style={{
            display: "grid",
            gridTemplateColumns: GRID_COLS,
            alignItems: "stretch",
            minWidth: 0,
            borderTop: "1px solid var(--kc-border-secondary)",
          }}
        >
          <div style={cellWrapStyle}>
            <Input
              variant="borderless"
              placeholder={colA.placeholder}
              value={r.a}
              onChange={(e) => update(idx, { a: e.target.value })}
              disabled={disabled}
              style={cellInputStyle}
            />
          </div>
          <div style={cellWrapStyle}>
            <Input
              variant="borderless"
              placeholder={colB.placeholder}
              value={r.b}
              onChange={(e) => update(idx, { b: e.target.value })}
              disabled={disabled}
              style={cellInputStyle}
            />
          </div>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
            <Button
              type="text"
              danger
              size="small"
              icon={<DeleteOutlined />}
              aria-label="Удалить строку"
              onClick={() => onChange(rows.filter((_, i) => i !== idx))}
              disabled={disabled}
            />
          </div>
        </div>
      ))}

      {/* footer — dashed add */}
      <div style={{ borderTop: "1px solid var(--kc-border-secondary)", padding: "8px 12px" }}>
        <Button
          type="dashed"
          block
          icon={<PlusOutlined />}
          onClick={() => onChange([...rows, { a: "", b: "" }])}
          disabled={disabled}
        >
          {addLabel}
        </Button>
      </div>
    </div>
  );
}
