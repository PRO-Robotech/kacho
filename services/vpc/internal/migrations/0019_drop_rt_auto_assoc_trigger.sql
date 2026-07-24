-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Снятие второго (последнего) DB-механизма выбора RouteTable для подсети.
-- =============================================================================
-- VPC-1 F8 требует прямо: «implementer обязан reconcile/удалить старый
-- trigger-выбор, НЕ оставлять два конкурирующих механизма выбора RT». 0017 снял
-- `subnet_auto_pick_rt` (BEFORE INSERT ON subnets, «самая ранняя RT сети»), но
-- оставил парный `rt_auto_assoc_subnets` (AFTER INSERT ON route_tables,
-- «усыновить все подсети сети с route_table_id IS NULL») — как «безобидный
-- backstop для legacy-сетей». Backstop'ом он не является:
--
--   * На всех путях, которые продукт производит после VPC-1, он строго no-op —
--     Network.Create безусловно провижнит системную RT и пишет
--     `networks.default_route_table_id`, а Subnet.Create подставляет её id,
--     поэтому подсети с route_table_id IS NULL просто не рождаются.
--   * Единственный достижимый путь, где он всё-таки СРАБАТЫВАЕТ, — деградация:
--     RouteTable.Delete по FK ON DELETE SET NULL обнуляет и привязку подсетей,
--     и `networks.default_route_table_id` (0017). После этого следующий
--     RouteTable.Create молча переклеивал бы осиротевшие подсети на себя, хотя
--     объявленный дефолт сети пуст. Получается ровно то, что запрещено: два
--     механизма решают, к какой RT привязана подсеть, причём второй невидим в
--     API и зависит от порядка создания.
--
-- Каноническим остаётся ОДИН механизм: привязка ставится на Subnet.Create из
-- явного `network.defaultRouteTableId°` и дальше меняется только явной мутацией
-- тенанта (`Subnet.Update` mask=route_table_id) либо FK SET NULL.
--
-- Что НЕ трогаем: `subnets_outbox_emit_route_table_change_trg` (AFTER UPDATE OF
-- route_table_id ON subnets) — он не выбирает RT, а лишь эмитит Subnet.UPDATED,
-- когда route_table_id меняет сама БД (теперь это только FK SET NULL). Функция
-- `kacho_vpc.rt_auto_assoc_subnets()` снимается вместе с триггером (LEAN:
-- оставленная функция — приглашение вернуть триггер следующей миграцией).
--
-- Миграции 0001/0017 при этом НЕ редактируются (ban #5) — снятие живёт здесь.

SET search_path TO kacho_vpc, public;

DROP TRIGGER IF EXISTS rt_auto_assoc_subnets_trg ON kacho_vpc.route_tables;
DROP FUNCTION IF EXISTS kacho_vpc.rt_auto_assoc_subnets();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

CREATE OR REPLACE FUNCTION kacho_vpc.rt_auto_assoc_subnets() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE kacho_vpc.subnets
       SET route_table_id = NEW.id
     WHERE network_id = NEW.network_id
       AND route_table_id IS NULL;
    RETURN NEW;
END;
$$;

CREATE TRIGGER rt_auto_assoc_subnets_trg
    AFTER INSERT ON kacho_vpc.route_tables
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.rt_auto_assoc_subnets();

-- +goose StatementEnd
