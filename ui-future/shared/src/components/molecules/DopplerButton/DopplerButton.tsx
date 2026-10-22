// DopplerButton — antd Button с радиальной "doppler" пульсацией (как
// loading-state в консоли). Используется на submit-кнопках Create-форм:
// после клика анимация идёт пока async Operation в pending-состоянии и
// гаснет когда op.done.
//
// Цвета — primary (синий ant-token), пульсация = два concentric expanding
// box-shadow-кольца (цвет-token + opacity-fade).

import { LoadingOutlined } from "@ant-design/icons";
import { Button } from "antd";
import type { ButtonProps } from "antd";

interface Props extends ButtonProps {
  /** Внешнее состояние ожидания (pending Operation). Анимация активна
   *  пока true; нажатия в это время не принимаются. */
  pulsing?: boolean;
}

export function DopplerButton({ pulsing, children, danger, loading, icon, onClick, ...rest }: Props) {
  // danger → пульсация тоном опасности; иначе — тоном сигнала. Цвет берётся
  // ИЗ ПАЛИТРЫ продукта и разбавляется прозрачностью на месте (`color-mix`).
  // Прежде тут стояли два литерала прежней палитры (#1677ff и #ff4d4f), не
  // менявшихся ни в одной теме: кольцо шло вокруг кнопки другого оттенка, а на
  // светлой теме и вовсе спорило с ней.
  //
  // Цвет кольца стережёт проба ЭТОГО файла; подвал формы (`organisms/form/
  // FormFooter`) утверждает только различимость `danger` и обычной отправки —
  // два места об одном цвете разъезжались бы на каждой правке палитры.
  const ring = danger ? "var(--kc-danger)" : "var(--kc-primary)";
  const ringStyle = {
    "--doppler-c": `color-mix(in srgb, ${ring} 55%, transparent)`,
    "--doppler-c0": "transparent",
  } as React.CSSProperties;
  // KAC-246: keyframes/стили вынесены в index.css (.doppler-btn). Inline <style>
  // убран — он рвал смежность .ant-btn+.ant-btn в Modal-футере (кнопки липли).
  // Теперь DopplerButton — чистый <Button>, и AntD-зазор между кнопками работает.
  //
  // Ожидание показывается СВОИМ значком, а не `loading` кнопки antd. Значок
  // `loading` antd уходит анимацией и снимается лишь по событию её конца; на
  // отказе сервера (ответ за доли секунды) это событие не приходило, и значок
  // оставался в кнопке нулевой ширины навсегда: кнопка называлась «loading
  // Создать», для читалки экрана висела в загрузке, а повторная отправка по
  // имени «Создать» её не находила (SC-52, console-e2e, kacho#3238). Свой значок
  // стоит ровно пока идёт ожидание и уходит вместе с ним — анимации ухода нет.
  // Защиту от повторной отправки, которую давал `loading`, держит обработчик ниже.
  const busy = Boolean(pulsing) || Boolean(loading);
  return (
    <Button
      {...rest}
      danger={danger}
      icon={busy ? <LoadingOutlined /> : icon}
      aria-busy={busy || undefined}
      aria-disabled={busy || undefined}
      onClick={(e) => {
        if (busy) {
          e.preventDefault();
          return;
        }
        onClick?.(e);
      }}
      style={pulsing ? { ...rest.style, ...ringStyle } : rest.style}
      className={[rest.className, "doppler-btn", pulsing && "is-pulsing"].filter(Boolean).join(" ")}
    >
      {children}
    </Button>
  );
}
