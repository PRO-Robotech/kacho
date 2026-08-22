// DopplerButton — antd Button с радиальной "doppler" пульсацией (как
// loading-state в консоли). Используется на submit-кнопках Create-форм:
// после клика анимация идёт пока async Operation в pending-состоянии и
// гаснет когда op.done.
//
// Цвета — primary (синий ant-token), пульсация = два concentric expanding
// box-shadow-кольца (цвет-token + opacity-fade).

import { Button } from "antd";
import type { ButtonProps } from "antd";

interface Props extends ButtonProps {
  /** Внешнее состояние ожидания (pending Operation). Анимация активна
   *  пока true. Заменяет/дополняет antd loading. */
  pulsing?: boolean;
}

export function DopplerButton({ pulsing, children, danger, ...rest }: Props) {
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
  return (
    <Button
      {...rest}
      danger={danger}
      loading={pulsing || rest.loading}
      style={pulsing ? { ...rest.style, ...ringStyle } : rest.style}
      className={[rest.className, "doppler-btn", pulsing && "is-pulsing"].filter(Boolean).join(" ")}
    >
      {children}
    </Button>
  );
}
