-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- default_route_table_id становится РЕАЛЬНЫМ источником истины (VPC-1 F3/F8).
-- =============================================================================
-- 0015 добавила колонку и объявила её «единственным источником истины, какой RT
-- дефолтный для сети… заменяет прежний trigger-выбор самый ранний RT», но кода,
-- который бы её писал, не существовало: тенант всегда видел пустое
-- `defaultRouteTableId°`, а RT новым подсетям по-прежнему выбирал триггер
-- `subnet_auto_pick_rt` (BEFORE INSERT ON subnets, «самая ранняя RT сети»).
-- Комментарий описывал не реальность, а намерение.
--
-- Теперь Network.Create провижнит системную RT в своей writer-TX и проставляет
-- её id (см. apps/kacho/api/network/default_rt.go), а Subnet.Create сам
-- подставляет `network.defaultRouteTableId°`, когда тенант не задал routeTableId.
-- Эта миграция закрывает две оставшиеся половины:
--
-- 1) BACKFILL существующих сетей. Дефолтом становится ровно та RT, которую
--    выбрал бы снимаемый триггер (самая ранняя по (created_at, id)) — семантика
--    для уже живущих сетей не меняется, просто перестаёт вычисляться на каждом
--    INSERT и становится явной. Сети без единой RT остаются с пустым значением
--    (указывать не на что; их подсети получают route_table_id IS NULL — ровно то
--    же, что вернул бы триггер из пустой выборки).
--
-- 2) СНЯТИЕ триггера subnet_auto_pick_rt. Он больше не должен участвовать в
--    выборе: иначе на сети с несколькими RT «дефолт» зависел бы от того, задал
--    ли клиент поле, и мог бы разойтись с объявленным defaultRouteTableId°.
--    Миграция 0001 при этом НЕ редактируется (ban #5) — триггер снимается здесь.
--
-- 3) FK на саму ссылку. Пока колонка была мёртвой, отсутствие FK ничего не
--    ломало; как только на неё опирается Subnet.Create, удаление RT оставило бы
--    dangling-дефолт — и КАЖДЫЙ Subnet.Create в этой сети падал бы на FK
--    subnets.route_table_id (23503). Полная симметрия с
--    networks_default_security_group_fk (0005): nullable + ON DELETE SET NULL,
--    NULL = «дефолта нет». Публичный контракт не меняется — repo читает колонку
--    через COALESCE(…,'') и пишет через NullableStr.
--
-- Триггер rt_auto_assoc_subnets (AFTER INSERT ON route_tables) СОХРАНЁН
-- намеренно: он усыновляет подсети, у которых route_table_id IS NULL, то есть
-- работает только для legacy-сетей без дефолта. С явным defaultRouteTableId°
-- новые подсети приходят уже с непустым route_table_id, поэтому он для них
-- no-op и с дефолтом не конкурирует.

SET search_path TO kacho_vpc, public;

UPDATE kacho_vpc.networks n
   SET default_route_table_id = (
        SELECT r.id
          FROM kacho_vpc.route_tables r
         WHERE r.network_id = n.id
         ORDER BY r.created_at ASC, r.id ASC
         LIMIT 1)
 WHERE n.default_route_table_id = ''
   AND EXISTS (SELECT 1 FROM kacho_vpc.route_tables r WHERE r.network_id = n.id);

-- Колонка становится nullable + FK ON DELETE SET NULL (симметрия 0005).
-- Порядок важен: backfill выше опирается на предикат '' (до конверсии в NULL).
ALTER TABLE kacho_vpc.networks ALTER COLUMN default_route_table_id DROP DEFAULT;
ALTER TABLE kacho_vpc.networks ALTER COLUMN default_route_table_id DROP NOT NULL;
UPDATE kacho_vpc.networks SET default_route_table_id = NULL WHERE default_route_table_id = '';
ALTER TABLE kacho_vpc.networks
    ADD CONSTRAINT networks_default_route_table_fk
    FOREIGN KEY (default_route_table_id)
    REFERENCES kacho_vpc.route_tables (id) ON DELETE SET NULL;

DROP TRIGGER IF EXISTS subnet_auto_pick_rt_trg ON kacho_vpc.subnets;
DROP FUNCTION IF EXISTS kacho_vpc.subnet_auto_pick_rt();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

ALTER TABLE kacho_vpc.networks DROP CONSTRAINT IF EXISTS networks_default_route_table_fk;
UPDATE kacho_vpc.networks SET default_route_table_id = '' WHERE default_route_table_id IS NULL;
ALTER TABLE kacho_vpc.networks ALTER COLUMN default_route_table_id SET DEFAULT '';
ALTER TABLE kacho_vpc.networks ALTER COLUMN default_route_table_id SET NOT NULL;

CREATE OR REPLACE FUNCTION kacho_vpc.subnet_auto_pick_rt() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.route_table_id IS NULL THEN
        SELECT id INTO NEW.route_table_id
          FROM kacho_vpc.route_tables
         WHERE network_id = NEW.network_id
         ORDER BY created_at ASC, id ASC
         LIMIT 1;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER subnet_auto_pick_rt_trg
    BEFORE INSERT ON kacho_vpc.subnets
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.subnet_auto_pick_rt();

-- Backfill не откатывается: значение default_route_table_id совпадает с тем, что
-- выбрал бы восстановленный триггер, поэтому поведение идентично, а обнуление
-- колонки стёрло бы и дефолты, проставленные Network.Create.

-- +goose StatementEnd
