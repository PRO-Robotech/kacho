// DopplerButton — antd Button с радиальной "doppler" пульсацией (как
// loading-state в YC console). Используется на submit-кнопках Create-форм:
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
  // danger → красная пульсация (delete-flow); иначе синяя (primary).
  const ringStyle = danger
    ? ({ "--doppler-c": "rgba(255, 77, 79, 0.6)", "--doppler-c0": "rgba(255, 77, 79, 0)" } as React.CSSProperties)
    : ({ "--doppler-c": "rgba(22, 119, 255, 0.55)", "--doppler-c0": "rgba(22, 119, 255, 0)" } as React.CSSProperties);
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
