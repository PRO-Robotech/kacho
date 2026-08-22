// src/components/form/FormFooter.tsx
// FormFooter — единый футер Create/Edit форм: основное действие (DopplerButton)
// + отмена. pending → пульсация + защита от повторной отправки. sticky=true
// делает футер липким (для длинных форм — действия всегда видны). От тела
// отделяется линией, а не заливкой: глубина в консоли делается тоном.
// Theme-aware (--kc-*): чисто и в DARK, и в LIGHT.
import type { CSSProperties } from "react";
import { Button } from "antd";
import { DopplerButton } from "@shared/components/molecules/DopplerButton";

interface Props {
  submitLabel: string;
  submitting: boolean;
  onSubmit: () => void;
  onCancel: () => void;
  sticky?: boolean;
  /** Danger-вариант submit-кнопки (для delete-flow). По умолчанию primary. */
  danger?: boolean;
  /** Блокировка submit (например requireNameConfirm не пройден). */
  submitDisabled?: boolean;
}

// Основное действие — НЕ сплошная заливка, а сигнальная линия с лёгким тоном:
// насыщенный цвет в консоли занимает меньше десятой части поверхности и стоит
// только там, где сообщает действие.
//
// Форма собрана парой `color`/`variant` самого AntD, а не выписана цветами:
// заливка, цвет текста и НАВЕДЕНИЕ приезжают из палитры темы, поэтому светлая
// тема получает ту же форму без второго набора значений. Выписанная пара
// «тёмный цвет / светлый цвет» разошлась бы с палитрой молча — правят её в
// одном месте, а кнопка остаётся с прежним синим.
//
// Границы у варианта `filled` нет by construction (`--ant-btn-border-color:
// transparent`), а эталон её держит — поэтому она возвращается ОДНОЙ
// переменной самой кнопки. Значение — доля сигнального цвета: полный цвет на
// границе спорит с заливкой и делает кнопку громче содержимого формы.
//
// Цена названа: браузер без `color-mix` не подставит значение, и граница
// возьмёт цвет текста кнопки — то есть станет заметнее, чем задумано, но
// кнопка останется собранной (заливка и текст от неё не зависят).
const PRIMARY_BORDER = {
  "--ant-btn-border-color": "color-mix(in srgb, var(--kc-primary) 48%, transparent)",
} as CSSProperties;

/**
 * Нижняя граница ширины основного действия — чтобы подвал не «прыгал».
 *
 * Подпись действия НЕ НАЗЫВАЕТ ПРЕДМЕТ: «Создать», не «Создать облачную сеть»
 * (решение владельца). Предмет уже назван заголовком формы прямо над кнопкой, и
 * повторять его здесь значит говорить дважды.
 *
 * Здесь стояло обратное утверждение — «подпись склоняет имя ресурса», — и после
 * смены подписи оно стало третьим местом об одном предмете, из которых верно
 * одно. Такой комментарий опаснее устаревшей строки: следующий читатель чинит
 * КОД под неверный текст.
 *
 * Нижняя граница ширины при этом нужна по-прежнему, и причина её не изменилась:
 * кнопка, ужатая по подписи, при переходе создание↔правка съёживается, и «Отменить»
 * переезжает под курсор туда, где секунду назад было подтверждение, — то есть
 * промах по кнопке стоит отменённой работы.
 *
 * Названо честно: это ПОЛ, а не равенство. Подпись длиннее пола кнопку
 * расширит — сделать иначе значило бы обрезать имя ресурса, ради склонения
 * которого всё и затевалось. Пол взят по самой длинной подписи создания среди
 * восьми ресурсов сети («Создать таблицу маршрутов»), поэтому расширение
 * остаётся исключением, а не правилом.
 */
const PRIMARY_MIN_WIDTH = 196;

export function FormFooter({ submitLabel, submitting, onSubmit, onCancel, sticky, danger, submitDisabled }: Props) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        borderTop: "1px solid var(--kc-border)",
        // Симметричный вертикальный padding — кнопки по центру (был 14/2, «висели»
        // у низа). Боковые 0 — кнопки выровнены по левому краю полей.
        paddingTop: 16,
        // paddingBottom 0 — нижний отступ под кнопками даёт карточка/панель
        // (FormShell padding 20 / main pane 24); иначе под кнопками копится
        // ~36px и выглядит странно. В sticky-band ниже возвращаем нижний отступ.
        paddingBottom: 0,
        paddingLeft: 0,
        paddingRight: 0,
        marginTop: 10,
        ...(sticky
          ? {
              // Sticky-band (create-страницы) — bleed к краям FormShell-карточки
              // (padding 20px 22px), чтобы полоса шла на всю ширину карточки, а не
              // вставкой; кнопки re-inset на 22 → выровнены с полями.
              // Фон непрозрачный: под липкой полосой проезжает содержимое формы.
              background: "var(--kc-container)",
              position: "sticky",
              bottom: 0,
              zIndex: 1,
              marginLeft: -22,
              marginRight: -22,
              marginBottom: -20,
              paddingLeft: 22,
              paddingRight: 22,
              paddingBottom: 16,
              // Радиус поверхности — 11 (см. .kc-surface): полоса обязана
              // повторять угол карточки, а не срезать его своим.
              borderBottomLeftRadius: 11,
              borderBottomRightRadius: 11,
            }
          : null),
      }}
    >
      <DopplerButton
        // Деструктивное действие — линия опасности и её же текст, заливка
        // только при наведении: удаление не должно выглядеть громче создания,
        // оно должно выглядеть ИНАЧЕ.
        color={danger ? "danger" : "primary"}
        variant={danger ? "outlined" : "filled"}
        danger={danger}
        onClick={onSubmit}
        pulsing={submitting}
        disabled={submitDisabled}
        style={
          danger
            ? { minWidth: PRIMARY_MIN_WIDTH }
            : { ...PRIMARY_BORDER, minWidth: PRIMARY_MIN_WIDTH }
        }
      >
        {submitLabel}
      </DopplerButton>
      <Button onClick={onCancel} disabled={submitting}>
        Отменить
      </Button>
    </div>
  );
}
