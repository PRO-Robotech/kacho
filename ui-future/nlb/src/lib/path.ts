// Helpers для работы с dotted-path (e.g. "spec.rules[0].direction") внутри nested object.

export function getByPath(obj: unknown, path: string): unknown {
  const keys = parsePath(path);
  let cur: unknown = obj;
  for (const k of keys) {
    if (cur == null) return undefined;
    if (typeof k === "number") {
      if (!Array.isArray(cur)) return undefined;
      cur = cur[k];
    } else {
      if (typeof cur !== "object") return undefined;
      cur = (cur as Record<string, unknown>)[k];
    }
  }
  return cur;
}

export function setByPath<T extends Record<string, unknown>>(obj: T, path: string, value: unknown): T {
  const keys = parsePath(path);
  if (keys.length === 0) return obj;
  const next = clone(obj) as Record<string, unknown>;
  let cur: Record<string, unknown> | unknown[] = next;
  for (let i = 0; i < keys.length - 1; i++) {
    const k = keys[i];
    const nextKey = keys[i + 1];
    if (typeof k === "number") {
      const arr = cur as unknown[];
      if (arr[k] == null) arr[k] = typeof nextKey === "number" ? [] : {};
      cur = arr[k] as Record<string, unknown> | unknown[];
    } else {
      const map = cur as Record<string, unknown>;
      if (map[k] == null) map[k] = typeof nextKey === "number" ? [] : {};
      cur = map[k] as Record<string, unknown> | unknown[];
    }
  }
  const last = keys[keys.length - 1];
  if (typeof last === "number") {
    (cur as unknown[])[last] = value;
  } else {
    (cur as Record<string, unknown>)[last] = value;
  }
  return next as T;
}

export function deleteByPath<T extends Record<string, unknown>>(obj: T, path: string): T {
  const keys = parsePath(path);
  if (keys.length === 0) return obj;
  const next = clone(obj) as Record<string, unknown>;
  let cur: Record<string, unknown> | unknown[] = next;
  for (let i = 0; i < keys.length - 1; i++) {
    const k = keys[i];
    if (typeof k === "number") {
      const arr = cur as unknown[];
      if (arr[k] == null) return next as T;
      cur = arr[k] as Record<string, unknown> | unknown[];
    } else {
      const map = cur as Record<string, unknown>;
      if (map[k] == null) return next as T;
      cur = map[k] as Record<string, unknown> | unknown[];
    }
  }
  const last = keys[keys.length - 1];
  if (typeof last === "number") {
    (cur as unknown[]).splice(last, 1);
  } else {
    delete (cur as Record<string, unknown>)[last];
  }
  return next as T;
}

function parsePath(path: string): (string | number)[] {
  // Поддержка: "a.b[0].c", "a[0]", "a.b"
  const out: (string | number)[] = [];
  const parts = path.split(".");
  for (const part of parts) {
    const m = /^([^[]+)((?:\[\d+\])*)$/.exec(part);
    if (!m) {
      out.push(part);
      continue;
    }
    const [, head, idx] = m;
    if (head) out.push(head);
    if (idx) {
      const matches = idx.matchAll(/\[(\d+)\]/g);
      for (const im of matches) out.push(parseInt(im[1], 10));
    }
  }
  return out;
}

function clone<T>(v: T): T {
  if (v == null) return v;
  if (Array.isArray(v)) return v.map(clone) as T;
  if (typeof v === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, vv] of Object.entries(v as Record<string, unknown>)) out[k] = clone(vv);
    return out as T;
  }
  return v;
}
