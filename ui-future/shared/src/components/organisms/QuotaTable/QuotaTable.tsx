// Таблица пределов — ОДНА на все витрины квот (#364, #622).
//
// Витрин две и предметы у них разные: пределы ПРОЕКТА и пределы ЛИЧНОСТИ. Вид
// же у них один и тот же — четвёрка «вид · предел · занято · источник», — и
// каждое решение в этой четвёрке нетривиально: почему «занято» иногда молчит,
// почему источник обязателен, почему имя вида показано с подсказкой. Вторая
// копия этих решений разошлась бы с первой на первой же правке, и пользователь
// прочитал бы разницу как другое место продукта (`ui.md` правило 3).
//
// Отличие витрин задаётся ПАРАМЕТРАМИ (`subject` при сборке строк, текст пустого
// состояния), а не вторым компонентом.

import { Tooltip, Typography } from "antd";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { type QuotaRow } from "@shared/lib/quota-view";

export interface QuotaTableProps {
  rows: QuotaRow[];
  loading: boolean;
  /** Что сказать, когда строк нет. Текст принадлежит витрине: у пределов
   *  проекта и у пределов личности пусто означает разное. */
  empty: string;
}

const COLUMNS: Column<QuotaRow>[] = [
  {
    header: "Ресурс",
    className: "font-medium",
    // Подсказка несёт ТОКЕН вида — тот самый, которым предел назван в контракте
    // и в обращении к администратору. Без него человеческое имя не связать с
    // тем, что придётся произнести, прося поднять предел.
    cell: (r) => (
      <Tooltip title={r.kind}>
        <span>{r.label}</span>
      </Tooltip>
    ),
  },
  { header: "Предел", cell: (r) => <span style={{ fontVariantNumeric: "tabular-nums" }}>{r.limit}</span> },
  {
    header: "Занято",
    // Значения нет — называем НОСИТЕЛЯ, а не рисуем прочерк: прочерк на месте
    // живого факта утверждает о ресурсе неправду, а ноль читался бы как
    // «ничего не создано», хотя счёт просто ведётся не здесь.
    cell: (r) =>
      r.used === null ? (
        <Typography.Text type="secondary">{r.carrierLabel}</Typography.Text>
      ) : (
        <span style={{ fontVariantNumeric: "tabular-nums" }}>{r.used}</span>
      ),
  },
  { header: "Кто задал предел", cell: (r) => <Typography.Text type="secondary">{r.source}</Typography.Text> },
];

export function QuotaTable({ rows, loading, empty }: QuotaTableProps) {
  return (
    <ResourceTable
      rows={rows}
      loading={loading && rows.length === 0}
      rowKey={(r) => r.kind}
      columns={COLUMNS}
      // Квоты приезжают целиком одним ответом (курсора у него нет), поэтому
      // порядок здесь честен.
      complete
      // Порядок задан сборкой строк (по имени вида) и устойчив: ответ порядка не
      // обещает, а страница поллится — показанный «как пришёл» список
      // переставлялся бы под курсором читателя.
      empty={empty}
    />
  );
}
