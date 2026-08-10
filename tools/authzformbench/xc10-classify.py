#!/usr/bin/env python3
"""Разбор отчёта authzformbench и отнесение по правилу §9 приёмки XC-10.

Величины правила объявлены ДО прогона (§13) и здесь не выводятся из чисел:
X = 1.5, опорная точка N = 100, порог каскада = 200 мс.

Разделитель ячеек — два и более пробела; внутри ячейки двух пробелов подряд не
бывает ни в одном её виде (число, «not-applicable: …», «not-run: …»), поэтому
разбиение однозначно. Фиксированная ширина колонки НЕ годится: `%-26s` длинную
ячейку не режет, а сдвигает соседние — первая редакция этого разбора именно так
и объявила объём «не измеренным», хотя он измерен у всех шести форм.
"""
import re
import sys

X = 1.5
REF_N = 100
CASCADE_MS = 200.0

ENGINE_FORMS = ["A-flat", "B-group", "C-role-relation", "D-container", "BCD-combined"]
FORM_E = "E-relational"
ALL_FORMS = ENGINE_FORMS + [FORM_E]
TIMED_41 = ["W1-grant", "W2-revoke-1", "W3-relabel-1", "W4-relabel-K",
            "R1-check", "R2-one-partition", "R3-page-full"]
OPS_41 = TIMED_41 + ["V-volume"]

path = sys.argv[1]
lines = open(path, encoding="utf-8").read().split("\n")

ns = []
cells = {}
vol = {}


def row_fields(row):
    parts = re.split(r"\s{2,}", row.strip())
    return parts[0], parts[1:]


i = 0
while i < len(lines):
    ln = lines[i]
    m = re.match(r"^── ([A-Za-z0-9\-]+) ─+$", ln)
    if m:
        op = m.group(1)
        ns_here = [int(x) for x in re.findall(r"N=(\d+)", lines[i + 1])]
        if not ns:
            ns = ns_here
        j = i + 2
        while j < len(lines) and lines[j].strip():
            form, flds = row_fields(lines[j])
            for k, n in enumerate(ns_here):
                cells[(form, n, op)] = flds[k] if k < len(flds) else "нет ячейки"
            j += 1
        i = j
        continue
    if ln.startswith("── V-volume ──"):
        j = i + 1
        while j < len(lines) and not lines[j].startswith("form"):
            j += 1
        ns_here = [int(x) for x in re.findall(r"N=(\d+)", lines[j])]
        j += 1
        while j < len(lines) and lines[j].strip():
            form, flds = row_fields(lines[j])
            for k, n in enumerate(ns_here):
                vol[(form, n)] = flds[k] if k < len(flds) else "нет ячейки"
            j += 1
        i = j
        continue
    i += 1

# перепись прочитанного — «ноль находок» обязано быть отличимо от «ноль прочитанного»
read_ops = sorted({op for (_, _, op) in cells})
print(f"# прочитано: точек N {ns}; операций {len(read_ops)} {read_ops}; "
      f"ячеек времени {len(cells)}; ячеек объёма {len(vol)}")
for f in ALL_FORMS:
    miss = [op for op in read_ops if (f, ns[0], op) not in cells]
    if miss:
        print(f"# ВНИМАНИЕ: у формы {f} не прочитаны операции {miss}")
print()


def times(txt):
    m = re.match(r"^([0-9.]+) \(([0-9.]+)\)", txt or "")
    return (float(m.group(1)), float(m.group(2))) if m else None


def volbytes(txt):
    m = re.match(r"^(\d+) стр / (\d+)Б \(\+(\d+) стр структ\)", txt or "")
    return (int(m.group(1)), int(m.group(2)), int(m.group(3))) if m else None


out = []
P = out.append
P(f"правило отнесения объявлено ДО прогона (§13): X={X}; опорная точка N={REF_N}; "
  f"порог каскада={CASCADE_MS:.0f} мс, применяется к ОБЕИМ формам")
P("")

per_point = {}
split_pct = []
split_vol = []
ndep = []

for n in ns:
    P(f"══ точка N={n} ══")
    cats = {}
    base_print = {}
    for op in OPS_41:
        if op == "V-volume":
            e = volbytes(vol.get((FORM_E, n), ""))
            base = {f: v for f in ENGINE_FORMS if (v := volbytes(vol.get((f, n), "")))}
            if e is None or len(base) != len(ENGINE_FORMS):
                miss = [f for f in ENGINE_FORMS if f not in base] + ([FORM_E] if e is None else [])
                cats[op] = ("база неполна", f"не измерены: {', '.join(miss)}")
                continue
            bf = min(base, key=lambda f: base[f][1])
            rf = min(base, key=lambda f: base[f][0])
            mb = base[bf][1]
            cat = "лучше" if e[1] * X < mb else ("хуже" if e[1] > mb * X else "в полосе")
            cats[op] = (cat, f"E GrantBytes={e[1]}Б, GrantTotal={e[0]} стр")
            base_print[op] = (f"min GrantBytes={mb}Б у {bf} → {mb / e[1]:.2f}× "
                              f"(min GrantTotal={base[rf][0]} стр у {rf}, категорию не задаёт)")
            if bf != rf:
                split_vol.append(f"N={n} V-volume: min GrantTotal у {rf} ({base[rf][0]} стр), "
                                 f"min GrantBytes у {bf} ({base[bf][1]}Б); категорию задаёт GrantBytes")
            continue

        e = times(cells.get((FORM_E, n, op), ""))
        base = {f: t for f in ENGINE_FORMS if (t := times(cells.get((f, n, op), "")))}
        if e is None or len(base) != len(ENGINE_FORMS):
            miss = [f"{f}: {cells.get((f, n, op), 'нет ячейки')}"
                    for f in ENGINE_FORMS if f not in base]
            if e is None:
                miss.append(f"{FORM_E}: {cells.get((FORM_E, n, op), 'нет ячейки')}")
            cats[op] = ("база неполна", "; ".join(miss))
            continue
        p50f = min(base, key=lambda f: base[f][0])
        p95f = min(base, key=lambda f: base[f][1])
        m50, m95 = base[p50f][0], base[p95f][1]
        b50, b95 = e[0] * X < m50, e[1] * X < m95
        w50, w95 = e[0] > m50 * X, e[1] > m95 * X
        cat = "лучше" if (b50 and b95) else ("хуже" if (w50 and w95) else "в полосе")
        cats[op] = (cat, f"E P50={e[0]} P95={e[1]}")
        base_print[op] = (f"min P50={m50} у {p50f} → {m50 / e[0]:.2f}×; "
                          f"min P95={m95} у {p95f} → {m95 / e[1]:.2f}×")
        if (b50 != b95) or (w50 != w95):
            split_pct.append(f"N={n} {op}: E P50={e[0]} против min {m50} у {p50f} ({m50 / e[0]:.2f}×), "
                             f"E P95={e[1]} против min {m95} у {p95f} ({m95 / e[1]:.2f}×) — "
                             f"перцентили расходятся, отнесено в полосу")

    for op in OPS_41:
        c, why = cats[op]
        P(f"  {op:<18} {c:<13} {why}")
        if op in base_print:
            P(f"  {'':<18} {'база:':<13} {base_print[op]}")
    per_point[n] = cats

    classified = [op for op in OPS_41 if cats[op][0] in ("лучше", "хуже", "в полосе")]
    incomplete = [op for op in OPS_41 if cats[op][0] == "база неполна"]
    if not classified:
        P(f"  ИСХОД N={n}: НЕ КЛАССИФИЦИРОВАНА — ни одна из восьми операций §4.1 "
          f"не получила классифицирующей категории")
    else:
        has_b = any(cats[op][0] == "лучше" for op in OPS_41)
        has_w = any(cats[op][0] == "хуже" for op in OPS_41)
        P(f"  ИСХОД N={n}: " + {(True, False): "1 — форма E выигрывает",
                                (False, True): "2 — форма E проигрывает",
                                (False, False): "3 — сопоставима",
                                (True, True): "4 — двусторонний размен"}[(has_b, has_w)])
    P(f"  корзины: лучше={sum(1 for op in OPS_41 if cats[op][0] == 'лучше')} "
      f"хуже={sum(1 for op in OPS_41 if cats[op][0] == 'хуже')} "
      f"в полосе={sum(1 for op in OPS_41 if cats[op][0] == 'в полосе')} "
      f"база неполна={len(incomplete)} (сумма {len(OPS_41)})")
    P(f"  операций с неполной базой: {', '.join(incomplete) if incomplete else 'ноль'}")
    P("")

for op in OPS_41:
    seen = {n: per_point[n][op][0] for n in ns}
    if len(set(seen.values())) > 1:
        ndep.append(f"{op}: " + ", ".join(f"N={n} → {c}" for n, c in seen.items()))

P("── SPLIT-PERCENTILES ──")
P("\n".join("  " + s for s in split_pct) or "  (ни одной)")
P("── SPLIT-VOLUME ──")
P("\n".join("  " + s for s in split_vol) or "  (ни одной)")
P("── N-DEPENDENT ──")
P("\n".join("  " + s for s in ndep) or "  (ни одной — категория одинакова на всех трёх точках)")
P("")
P("── C-cascade: порог 200 мс к ОБЕИМ формам ──")
worst = {}
for n in ns:
    for f in ALL_FORMS:
        t = times(cells.get((f, n, "C-cascade"), ""))
        txt = cells.get((f, n, "C-cascade"), "нет ячейки")
        if t:
            worst[f] = max(worst.get(f, 0), t[1])
            P(f"  N={n:<5} {f:<18} P50={t[0]:<6} P95={t[1]:<6} "
              f"{'ПРЕВЫШЕН' if t[1] > CASCADE_MS else 'в пороге'}")
        else:
            P(f"  N={n:<5} {f:<18} {txt}")
P("  худший P95 по формам: " + ", ".join(f"{f}={v}" for f, v in worst.items()))
P("  порог превышен: " + (", ".join(f for f, v in worst.items() if v > CASCADE_MS) or "ни одной формой"))
P("")
P("── T-inline (§4.3; в шаг 1 не входят) ──")
for op in ("T-inline-grant", "T-inline-revoke"):
    for n in ns:
        eng = {cells.get((f, n, op), "") .split(":")[0] for f in ENGINE_FORMS}
        P(f"  {op:<16} N={n:<5} движок: {sorted(eng)}; E: {cells.get((FORM_E, n, op), 'нет ячейки')}")

print("\n".join(out))
