// Re-export of the single shared implementation. See shared/src/pages.
// Kept as a thin shim so the vpc remote's route imports (@/pages/*) resolve
// to one source of truth shared with the other remotes.
export * from "@shared/pages/QuotasPage";
