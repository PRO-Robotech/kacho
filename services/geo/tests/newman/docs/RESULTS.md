# Results — kacho-geo newman

## Статус

- **Авторинг:** 22 кейса (`cases/region.py` × 11, `cases/zone.py` × 11) —
  добавлены с нуля (geo ранее не имел newman-suite; был только placeholder README +
  tracked issue). Структура/DSL воспроизводят эталон `kacho-vpc/tests/newman`.
- **Syntax-gates (локально, без сети):**
  - `python3 scripts/gen.py` → 2 коллекции сгенерированы (`collections/region.postman_collection.json`,
    `collections/zone.postman_collection.json`), JSON well-formed (jq-valid).
  - `python3 scripts/validate-cases.py` → **OK** (22 уникальных case-id, нет дублей, все
    каталогизированы в `CASES-INDEX.md`).
- **Newman-исполнение:** локально env-blocked (харнесс убивает port-forward — см.
  `.claude` MEMORY «Local newman env blocked»). Исполнение — CI против задеплоенного стека.

## Ожидаемый вердикт (на CI-стеке `redesign/integration @ 8f3dca1`)

Все 22 кейса — **ожидаются зелёными**: они авторены против фактически задеплоенного
AS-IS-контракта (сверено с proto/gateway/serviceerr в дереве), с forward-compatible
ассертами через границу GEO-1-редизайна (см. `TEST-PLAN.md` §probe). Ключевые инварианты:
happy read (viewer→200), malformed→400, absent→404 verbatim, pagination-validate→400,
anonymous→401, two-projection NotContains infra/host-class, admin-write-not-on-public.

## Known failing — product bugs

**Нет.** Suite не содержит красных кейсов против реальных багов прода. Все находки при
probe — это **не приземлённый redesign** (GEO-1 pending), не дефекты: они вынесены в
`TEST-PLAN.md` §Deferred (добавляются GEO-1 PR по DoD-трассировке), а НЕ заведены как
GitHub-issue `bug` (feature-work, не баг) и НЕ авторены как заведомо-красные кейсы.

## Deferred (redesign-gated) — НЕ включены в suite

См. `TEST-PLAN.md` §"Deferred — GEO-1 redesign": GEO-1-20 (EXEMPT zero-binding→200),
GEO-1-02/03/06..09 (two-projection status→Internal, countryCode°/openForPlacement°),
GEO-1-16..19 (`/geo/v1/internal/…` admin-CRUD), GEO-1-34/36/37 (internal-mux admin negatives).
Каждый — прямая единица работы GEO-1-PR (`# verifies GEO-1-NN`), приземляется вместе с кодом.
