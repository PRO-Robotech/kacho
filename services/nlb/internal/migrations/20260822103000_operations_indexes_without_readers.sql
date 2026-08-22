-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- 20260822103000_operations_indexes_without_readers — два индекса таблицы
-- операций сняты: спрашивающего у них нет (kacho#913).
--
-- ───────────────────────────────────────────────────────────────────────────
-- ЗАМЕР, А НЕ ВПЕЧАТЛЕНИЕ
--
-- Общий список операций фильтрует РОВНО по двум полям — `resource_id` и
-- `account_id` (`pkg/operations` `ListFilter`), и своего запроса к таблице
-- операций у nlb нет ни одного (предикат: обход `services/nlb/internal/repo`
-- по имени таблицы даёт пусто).
--
-- Против этого:
--
--   operations_project_created_idx    ведёт по (project_id, created_at, id)
--   operations_resource_history_idx   ведёт по (resource_type, resource_id, …)
--
-- Первый бесполезен by construction: фильтра по проекту у списка нет вовсе.
-- Второй непригоден для единственного подходящего фильтра: его ведущая колонка
-- — `resource_type`, а запрос называет только `resource_id`, поэтому планировщик
-- этот индекс не возьмёт.
--
-- ───────────────────────────────────────────────────────────────────────────
-- ПОЧЕМУ ЭТО НЕ «ПРОСТО ЛИШНЕЕ МЕСТО»
--
-- Индекс без спрашивающего оплачивается КАЖДОЙ записью в таблицу: очередь
-- операций пишется на пути запроса, и цена ложится на мутацию, ради которой
-- операция и заводится. Плюс он читается как утверждение — «этот запрос у нас
-- есть», — и следующий, кто станет искать, почему список медленный, найдёт
-- «подходящий» индекс и не поймёт, отчего тот не выбирается.
--
-- ───────────────────────────────────────────────────────────────────────────
-- ЧТО ВЕРНЁТ ИХ ОБРАТНО
--
-- Появление у nlb собственного чтения операций с фильтром по проекту либо по
-- паре (тип, идентификатор). Тогда индекс заводится ВМЕСТЕ с этим чтением, а не
-- вперёд него: индекс вперёд читателя — то же обещание, что и снимается сейчас.
--
-- CONCURRENTLY: таблица пишется на пути запроса, и останавливать её запись ради
-- снятия индекса нечем оправдать. `IF EXISTS` делает повтор безопасным.

-- +goose NO TRANSACTION
-- +goose Up

DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.operations_project_created_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.operations_resource_history_idx;

-- +goose Down

CREATE INDEX CONCURRENTLY IF NOT EXISTS operations_project_created_idx
    ON kacho_nlb.operations (project_id, created_at DESC, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS operations_resource_history_idx
    ON kacho_nlb.operations (resource_type, resource_id, created_at DESC)
    WHERE resource_id IS NOT NULL;
