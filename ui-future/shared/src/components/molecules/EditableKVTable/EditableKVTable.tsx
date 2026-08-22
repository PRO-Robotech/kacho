// EditableKVTable — управляемый редактор набора строк: таблица значений, ⌫ на
// строке и кнопка добавления снизу. На нём собраны метки ресурса, статические
// маршруты формы и списки из одного текстового подполя (CIDR в форме создания).
//
// Геометрия берётся из общего `editor-surface` — того же, по которому нарисованы
// секция CIDR карточки, редактор маршрутов и панель маршрутов. Прежде она жила
// здесь своя: строка 38 вместо 42, радиус 8 вместо 11, колонка действий 40
// вместо общей, шапка со своим отступом и своим тоном, разделитель строк
// вторичной линией вместо основной. Расхождение видно только рядом на одном
// экране — то есть почти никогда, — и потому оно росло: пять копий одной высоты
// строки расходятся молча.
//
// Что здесь ОСТАЁТСЯ своим и почему это названо, а не унаследовано:
//
//   * заливка — `--kc-field`, а не поверхность карточки. Виджет стоит В ФОРМЕ,
//     среди полей ввода, и обязан читаться как такой же контрол; на карточке
//     ресурса тон полей и тон поверхности — два РАЗНЫХ тона палитры, и общий
//     сделал бы виджет светлее соседних полей;
//   * кнопка добавления — пунктирная во всю ширину, а не «поле + Добавить».
//     Здесь добавляют СТРОКУ, которую потом заполняют, а не готовое значение:
//     поля, куда его вводить, у составной строки нет одного.
//
// div-grid (minmax(0,1fr)) — не <table>: виджет обязан сжиматься и не выталкивать
// разметку формы (KAC-246 gotcha).
import { Button, Input } from "antd";
import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { FieldError } from "@shared/components/organisms/form/FieldError";
import {
  EDITOR_ACTIONS_WIDTH,
  EDITOR_ROW_HEIGHT,
  MONO_FONT,
  editorHeadCellStyle,
  editorIconButtonStyle,
  editorSurfaceStyle,
} from "@shared/components/organisms/form/editor-surface";

export interface KVRow {
  a: string;
  b: string;
}

interface ColDef {
  header: string;
  placeholder: string;
}

/**
 * Отказ, относящийся к ОДНОЙ строке набора.
 *
 * Идентификатор приходит извне, а не выводится здесь: на него ссылается
 * `aria-describedby` того самого ввода, и правило вывода принадлежит форме
 * (`fieldErrorId`), а не таблице. Своё правило здесь означало бы два места об
 * одном предмете — и разошлись бы они молча, потому что висячую ссылку
 * `aria-describedby` глазами не видно.
 */
export interface KVRowError {
  id: string;
  message: string;
}

interface Props {
  rows: KVRow[];
  onChange: (rows: KVRow[]) => void;
  colA: ColDef;
  /** Вторая колонка. Не задана — таблица одноколоночная: значение + действие.
   *  Нужна спискам, где у элемента ровно одно поле (CIDR сети, CIDR подсети). */
  colB?: ColDef;
  addLabel: string;
  disabled?: boolean;
  /**
   * Отказы по строкам — по индексу строки, `undefined` там, где претензий нет.
   *
   * Сообщение стоит В СТРОКЕ, к которой относится, а не под таблицей и не у
   * поля целиком (решение владельца: «невведённые поля подсвечивать там, где не
   * ввели поле, а не сбоку»). Пока этого места не было, отказ подполя строки
   * не показывался НИГДЕ: форма отказывалась отправляться и молчала о причине —
   * нажатие на «Создать» не давало ни ответа, ни объяснения.
   *
   * Помечается негодным ввод ПЕРВОЙ колонки: она несёт значение строки, и в
   * единственном месте, откуда отказы приходят (список из одного подполя),
   * второй колонки нет вовсе.
   */
  rowErrors?: (KVRowError | undefined)[];
  /** Не рисовать шапку колонок. Нужно в ФОРМЕ: там имя поля уже стоит слева, и
   *  заголовок единственной колонки повторял бы его вторым словом
   *  («IPv4-адрес» слева и «Address» в шапке — находка владельца 2026-08-12). */
  hideHeader?: boolean;
}

const ROW_H = EDITOR_ROW_HEIGHT;
const GRID_COLS_2 = `minmax(0, 1fr) minmax(0, 1fr) ${EDITOR_ACTIONS_WIDTH}px`;
const GRID_COLS_1 = `minmax(0, 1fr) ${EDITOR_ACTIONS_WIDTH}px`;

const cellInputStyle: React.CSSProperties = {
  width: "100%",
  minWidth: 0,
  background: "transparent",
  fontFamily: MONO_FONT,
  fontSize: 11,
  fontWeight: 520,
  padding: 0,
  height: ROW_H - 1,
  lineHeight: `${ROW_H - 1}px`,
};

/** Разделитель КОЛОНОК — вторичная линия: он делит одну строку, а не строки. */
const COL_DIVIDER = "1px solid var(--kc-border-secondary)";

const headCellStyle: React.CSSProperties = {
  ...editorHeadCellStyle,
  display: "flex",
  alignItems: "center",
  borderRight: COL_DIVIDER,
};

/**
 * Линия между соседями рисуется ОДИН раз: у шапки она снизу, у строки — сверху,
 * и на стыке они дали бы 2px там, где по всей консоли 1px. Сетка линии не
 * схлопывает — в отличие от таблицы, где это делает `border-collapse`. Поэтому
 * первая строка своей линии не несёт: сверху её держит либо шапка, либо рамка
 * самой поверхности.
 */
const ROW_TOP_LINE = "1px solid var(--kc-border)";

const cellWrapStyle: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  padding: "0 16px",
  minWidth: 0,
  borderRight: COL_DIVIDER,
};

export function EditableKVTable({ rows, onChange, colA, colB, addLabel, disabled, hideHeader, rowErrors }: Props) {
  const GRID_COLS = colB ? GRID_COLS_2 : GRID_COLS_1;
  const update = (idx: number, patch: Partial<KVRow>) => {
    onChange(rows.map((row, i) => (i === idx ? { ...row, ...patch } : row)));
  };

  return (
    <div style={{ ...editorSurfaceStyle, width: "100%", minWidth: 0, background: "var(--kc-field)" }}>
      {hideHeader ? null : (
        <div style={{ display: "grid", gridTemplateColumns: GRID_COLS }}>
          <div style={headCellStyle}>{colA.header}</div>
          {colB ? <div style={headCellStyle}>{colB.header}</div> : null}
          <div style={{ ...editorHeadCellStyle, padding: 0 }} />
        </div>
      )}

      {/* Пустой набор рисуется без строк: у него есть кнопка добавления, и она
          сама говорит, чего здесь пока нет. */}
      {rows.map((r, idx) => {
        const problem = rowErrors?.[idx];
        return (
        // Обёртка строки — та единица, внутри которой стоят и ввод, и отказ о
        // нём. Линия-разделитель переехала сюда со строки-сетки: она делит
        // СОСЕДЕЙ, а сообщение принадлежит строке над ним и отделяться от неё
        // не должно.
        <div key={idx} style={{ borderTop: idx === 0 ? "none" : ROW_TOP_LINE }}>
        <div
          className="kc-kv-row"
          style={{
            display: "grid",
            gridTemplateColumns: GRID_COLS,
            alignItems: "stretch",
            minWidth: 0,
            minHeight: ROW_H,
          }}
        >
          <div style={cellWrapStyle}>
            <Input
              variant="borderless"
              placeholder={colA.placeholder}
              value={r.a}
              onChange={(e) => update(idx, { a: e.target.value })}
              disabled={disabled}
              aria-invalid={problem ? true : undefined}
              aria-describedby={problem?.id}
              style={cellInputStyle}
            />
          </div>
          {colB ? (
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
          ) : null}
          <div style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
            <Button
              type="text"
              danger
              icon={<DeleteOutlined />}
              aria-label="Удалить строку"
              onClick={() => onChange(rows.filter((_, i) => i !== idx))}
              disabled={disabled}
              style={editorIconButtonStyle}
            />
          </div>
        </div>
        {/* Отказ — под своей строкой и в её же обёртке: читатель находит
            незаполненный ввод там, где читает претензию, а не ищет его глазами
            по всему набору. Отступы держат сообщение в колонке значения. */}
        {problem ? (
          <div style={{ padding: "0 16px 8px" }}>
            <FieldError id={problem.id} message={problem.message} />
          </div>
        ) : null}
        </div>
        );
      })}

      <div style={{ borderTop: rows.length === 0 ? "none" : ROW_TOP_LINE, padding: 10 }}>
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
