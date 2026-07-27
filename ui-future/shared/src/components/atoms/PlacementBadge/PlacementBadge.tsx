// PlacementBadge — openForPlacement° for a Region or a Zone.
//
// Three answers, not two. `true` and `false` are what the server said; an absent
// field is what it did not say, and that is rendered as "—" rather than as
// "закрыт" — a missing signal is not a refusal.
//
// A Zone additionally carries placementBlockedReason, resolved by the service in
// the same call (zone down wins over region down). It is only meaningful for a
// closed row, and only for a reason this build knows: an unrecognised one is
// left unsaid rather than printed raw.

import type { CSSProperties } from "react";
import { openForPlacementLabel, placementBlockedText, type PlacementBlockedReason } from "@shared/api/geo";

const TONE_STYLE: Record<string, CSSProperties> = {
  open: { background: "var(--status-ok-bg)", color: "var(--status-ok-fg)", borderColor: "var(--status-ok-border)" },
  closed: {
    background: "var(--status-warn-bg)",
    color: "var(--status-warn-fg)",
    borderColor: "var(--status-warn-border)",
  },
  unknown: {
    background: "var(--status-muted-bg)",
    color: "var(--status-muted-fg)",
    borderColor: "var(--status-muted-border)",
  },
};

interface Props {
  open: boolean | undefined;
  reason?: PlacementBlockedReason;
}

export function PlacementBadge({ open, reason }: Props) {
  const label = openForPlacementLabel(open);
  const cause = open === false ? placementBlockedText(reason) : null;

  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <span
        className="inline-flex items-center rounded px-1.5 h-[20px] text-[11px] font-medium leading-none border"
        style={TONE_STYLE[label.tone]}
      >
        {label.text}
      </span>
      {cause && <span className="text-[11px] text-muted-foreground">{cause}</span>}
    </span>
  );
}
