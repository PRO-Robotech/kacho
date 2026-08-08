-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- Базовый каталог размещения kacho-geo для ПОДНЯТОГО стенда: регионы + зоны,
-- открытые для размещения. Применяется скриптом deploy/scripts/seed-geo-baseline.sh
-- (цель `make seed-geo`, вызывается из рецепта `dev-up`).
--
-- ЗАЧЕМ. Каталог geo стартует ПУСТЫМ — это осознанное решение
-- (services/geo/internal/migrations/0001_initial.sql: «встроенного seed нет»,
-- Region/Zone заводит администратор). Пока каталог пуст, ЛЮБОЕ создание
-- зонального или регионального ресурса отвечает «зона/регион не найдены»:
-- vpc/compute/nlb/storage/registry валидируют zoneId/regionId peer-вызовом к geo
-- на пути запроса, fail-closed. То есть свежеподнятый стенд без этого шага
-- непригоден, и это не видно ни по одному «под Ready».
--
-- ПОЧЕМУ SQL, А НЕ ADMIN-RPC. Через админский RPC каталог сеет приёмочная
-- фикстура (tests/authz-fixtures/prodseed_matrix.py::_seed_geo_catalog) — ей
-- нужен бутстрап-токен, Hydra и проброс портов. Повторять эту машинерию в
-- Makefile значило бы завести её ВТОРУЮ копию, которая разъедется молча.
-- Здесь это законная замена, и вот предикат: Region/Zone НЕ несут пообъектного
-- tuple в модели прав — `git grep -c RegisterResource -- 'services/geo/**/*.go'`
-- даёт 0 файлов (зеркальная форма на compute даёт 14, то есть предикат читает
-- то, что должен), а services/geo/docs/architecture/authz-and-tuples.md заявляет
-- это решением: «geo намеренно НЕ участвует в потоке owner-таплов». Публичное
-- чтение объявлено project-scope EXEMPT, поэтому строка, записанная
-- SQL-ом, для КАЖДОГО читателя тождественна записанной через RPC. Единственное
-- наблюдаемое отличие — audit-строка geo_outbox, и она пишется здесь же, в том
-- же операторе, ровно той формы, что пишет репозиторий
-- (services/geo/internal/repo/kacho/pg/{region,zone}.go, outbox.Emit).
--
-- ИДЕМПОТЕНТНОСТЬ. `ON CONFLICT DO NOTHING` БЕЗ указания цели — накрывает и PK
-- id, и глобальную UNIQUE(name) (обе завёл 0004). Audit-строка привязана к
-- RETURNING вставки, поэтому повторный прогон не пишет ни ресурсной, ни
-- audit-строки: число строк geo_outbox после N прогонов равно числу после
-- первого. Это и есть проверяемый предикат идемпотентности — он в
-- TestStandGeoBaselineSeedIsIdempotent.
--
-- ЧТО СЕЕТСЯ. Ровно те идентификаторы, которые приёмочные фикстуры называют
-- своими (`existingRegionId`/`existingRegionAltId`/`existingZoneId`/
-- `existingZoneAltId` в prodseed_matrix.py) — иначе стенд был бы «пригоден», но
-- не для тех кейсов, ради которых поднят. Согласованность двух артефактов
-- держит тест TestStandGeoBaselineSeedCoversFixtureIds, а не эта строка.
--
-- ACTOR в audit-payload — `stand_seed:make-seed-geo`: строку записал подъём
-- стенда, а не принципал. Маркер назван честно; `unknown` здесь был бы ложью
-- об утраченной атрибуции (её не утрачивали — её не было).

SET search_path TO kacho_geo, public;

-- Регионы. status='UP' — каталог заводится ОТКРЫТЫМ намеренно: стенд поднимают,
-- чтобы на нём размещать. Боевой каталог открывает администратор явным Update
-- (fail-safe DEFAULT 'DOWN' в 0004 остаётся для строк, заведённых не отсюда).
WITH seed(id, name) AS (
    VALUES ('ru-central1', 'ru-central1'),
           ('ru-central2', 'ru-central2')
), ins AS (
    INSERT INTO regions (id, name, country_code, status, numeric_infra_id, created_at)
    SELECT id, name, '', 'UP', 0, now() FROM seed
    ON CONFLICT DO NOTHING
    RETURNING id, name, country_code, status
)
INSERT INTO geo_outbox (resource_kind, resource_id, event_type, payload)
SELECT 'Region', id, 'CREATED',
       jsonb_build_object('id', id, 'name', name, 'country_code', country_code,
                          'status', status, 'actor', 'stand_seed:make-seed-geo')
FROM ins;

-- Зоны. Порядок обязателен: FK RESTRICT zones.region_id → regions(id).
WITH seed(id, region_id, name) AS (
    VALUES ('ru-central1-a', 'ru-central1', 'ru-central1-a'),
           ('ru-central1-b', 'ru-central1', 'ru-central1-b'),
           ('ru-central1-c', 'ru-central1', 'ru-central1-c'),
           ('ru-central1-d', 'ru-central1', 'ru-central1-d'),
           ('ru-central1-e', 'ru-central1', 'ru-central1-e'),
           ('ru-central2-a', 'ru-central2', 'ru-central2-a')
), ins AS (
    INSERT INTO zones (id, region_id, name, status,
                       numeric_infra_id, host_classes, failure_domain_count,
                       underlay_anchor, capacity_hint, created_at)
    SELECT id, region_id, name, 'UP', 0, '{}'::text[], 0, '', '', now() FROM seed
    ON CONFLICT DO NOTHING
    RETURNING id, region_id, name, status
)
INSERT INTO geo_outbox (resource_kind, resource_id, event_type, payload)
SELECT 'Zone', id, 'CREATED',
       jsonb_build_object('id', id, 'region_id', region_id, 'name', name,
                          'status', status, 'actor', 'stand_seed:make-seed-geo')
FROM ins;
