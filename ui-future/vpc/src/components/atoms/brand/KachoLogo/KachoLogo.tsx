// Бренд-знак Kachō — «лепестки-вертушка» (花鳥): 4 грани-крыла, двухградиентная
// вертушка (blue→violet / teal→indigo). Inline SVG — чёткий на любом размере,
// без сетевого запроса. variant="full" добавляет вордмарк «Kachō».

export interface KachoLogoProps {
  size?: number;
  variant?: "mark" | "full";
  /** Цвет вордмарка. По умолчанию currentColor (наследует от хедера). */
  wordmarkColor?: string;
  className?: string;
  style?: React.CSSProperties;
}

/** Чистый знак (без вордмарка) — лепестки-вертушка, inline SVG. */
function Mark({ size }: { size: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 48 48" style={{ display: "block" }} role="img" aria-label="Kachō">
      <defs>
        <linearGradient id="kachoLogoG" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#3D8DF5" />
          <stop offset="1" stopColor="#7B6CF6" />
        </linearGradient>
        <linearGradient id="kachoLogoG2" x1="0" y1="1" x2="1" y2="0">
          <stop offset="0" stopColor="#2BB5C0" />
          <stop offset="1" stopColor="#5B7CFA" />
        </linearGradient>
      </defs>
      <g transform="translate(24 24)">
        <path d="M0 0 L0 -20 A8 8 0 0 1 14 -14 Z" fill="url(#kachoLogoG)" />
        <path d="M0 0 L20 0 A8 8 0 0 1 14 14 Z" fill="url(#kachoLogoG2)" />
        <path d="M0 0 L0 20 A8 8 0 0 1 -14 14 Z" fill="url(#kachoLogoG)" opacity="0.8" />
        <path d="M0 0 L-20 0 A8 8 0 0 1 -14 -14 Z" fill="url(#kachoLogoG2)" opacity="0.8" />
      </g>
    </svg>
  );
}

export function KachoLogo({
  size = 24,
  variant = "mark",
  wordmarkColor = "currentColor",
  className,
  style,
}: KachoLogoProps) {
  if (variant === "mark") {
    return (
      <span className={className} style={{ display: "inline-flex", alignItems: "center", lineHeight: 0, ...style }}>
        <Mark size={size} />
      </span>
    );
  }

  return (
    <span
      className={className}
      style={{ display: "inline-flex", alignItems: "center", gap: 8, lineHeight: 0, ...style }}
    >
      <Mark size={size} />
      <span
        style={{
          fontFamily:
            "Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
          fontWeight: 600,
          fontSize: Math.round(size * 0.66),
          letterSpacing: "-0.01em",
          color: wordmarkColor,
          lineHeight: 1,
        }}
      >
        Kachō
      </span>
    </span>
  );
}

export default KachoLogo;
