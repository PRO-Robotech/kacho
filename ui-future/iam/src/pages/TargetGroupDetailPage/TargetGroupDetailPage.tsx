// Re-export of the single shared implementation. See shared/src/pages.
// Kept as a thin shim so the iam remote's imports (@/pages/*) resolve to one
// source of truth shared with the vpc remote (no auth/page-logic drift).
export * from "@shared/pages/TargetGroupDetailPage";
