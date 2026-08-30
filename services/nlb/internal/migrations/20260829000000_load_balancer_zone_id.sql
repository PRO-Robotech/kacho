-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- LoadBalancer zone_id — площадка ZONAL-балансировщика (задача продукта #1473)
-- =============================================================================
-- Публичная проекция несла placement_type ∈ {ZONAL, REGIONAL} и не несла зоны:
-- про INTERNAL_ZONAL можно было сказать «размещение зональное» и нельзя —
-- КАКОЕ. Площадка, на которой машина и балансировщик обязаны совпасть
-- (data-integrity.md §Placement-coherence), оставалась неназванной.
--
-- Форма взята у канонического якоря размещения — Subnet: дискриминатор
-- placement_type + взаимоисключающие координаты. У балансировщика region_id
-- обязателен при ЛЮБОМ размещении (зональный тоже принадлежит региону), поэтому
-- исключительность выражается парой placement_type ↔ zone_id, а не тройкой.
--
-- Значение приходит РЕЗОЛВОМ подсети VIP у её владельца (vpc.SubnetService.Get)
-- на пути запроса — там же, где уже сверяется согласие семейств. Из имени зоны
-- оно не выводится никогда: строковая деривация молча отдаёт пустую строку на
-- REGIONAL-подсети и превращает проверку когерентности в тождественно истинную.
--
-- БЕЗОПАСНОСТЬ. Наружу выходит зона СОБСТВЕННОЙ подсети арендатора — той, что он
-- сам назвал источником VIP и читает у vpc. Зона, которую платформа выбирает
-- сама (внешний public-VIP), остаётся скрытой: при EXTERNAL строка пуста. Это
-- ровно та граница, из-за которой сняты прежние per-zone-поля контракта
-- (reserved 15, 18), и держит её CHECK ниже, а не комментарий.

-- DEFAULT '' даёт instant metadata-only ALTER (без переписывания таблицы).
ALTER TABLE kacho_nlb.load_balancers
    ADD COLUMN IF NOT EXISTS zone_id text NOT NULL DEFAULT '';

-- Дискриминатор: непустая зона РОВНО при placement_type='ZONAL'.
--   ZONAL без зоны        — то состояние, ради снятия которого поле заведено;
--   REGIONAL с зоной      — выдуманная координата (anycast зоны не имеет);
--   EXTERNAL с зоной      — утечка размещения (зону выбирает платформа).
--
-- NOT VALID — таблица наполнена, а зона живёт у ВЛАДЕЛЬЦА подсети (vpc), и
-- восстановить её для уже созданных строк внутри этой базы нечем: SQL до
-- соседа не дотягивается. Ретроспективно не валидируются только они; на все
-- новые и изменяемые строки энфорс действует. Та же посадка и по той же причине
-- у load_balancers_placement_type_check (0011).
ALTER TABLE kacho_nlb.load_balancers
    ADD CONSTRAINT load_balancers_zone_placement_check
    CHECK ((placement_type = 'ZONAL') = (zone_id <> '')) NOT VALID;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kacho_nlb.load_balancers
    DROP CONSTRAINT IF EXISTS load_balancers_zone_placement_check;
ALTER TABLE kacho_nlb.load_balancers
    DROP COLUMN IF EXISTS zone_id;

-- +goose StatementEnd
