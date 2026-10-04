-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Вид журнала nlb принимает ключ сигнала ленты `notification`.
-- =============================================================================
-- Задача `PRO-Robotech/kacho#2918` (полоса S1-A1); приёмка
-- `docs/specs/sub-phase-NTF-3-kacho-modules-notifications-acceptance.md`, §1.9,
-- Д3, §3 шаг (3); замысел З5.
--
-- ЗАЧЕМ. Строку сигнала ленты модуля (объект `notification_feed:nlb`) пишет тот же
-- писатель журнала, что и строки ресурсов, с видом `notification` — ключом журнала
-- `corelib/notify/feed.JournalKey`. У четырёх соседних журналов колонка вида
-- открыта; у nlb она закрыта ограничением `nlb_outbox_resource_type_check`, и без
-- этого слова строка сигнала nlb была бы отвергнута базой `23514`.
--
-- СЛОВАРЬ ОСТАЁТСЯ ЗАКРЫТЫМ: добавляется ровно одно слово, постороннее по-прежнему
-- отвергается тем же ограничением с тем же именем. Род изменения строки сигнала
-- берётся из словаря журнала владельца, поэтому `nlb_outbox_action_check` не
-- меняется.
--
-- ПОЧЕМУ СНЯТЬ И ПОСТАВИТЬ, А НЕ ПРАВИТЬ `0001_initial.sql`. Применённая миграция
-- не правится (ban #5). Имя ограничения сохраняется: по нему отказ узнают
-- вызывающие и пробы. Снятие и постановка идут одной транзакцией миграции, окна
-- без ограничения нет.

-- +goose Up
SET search_path TO kacho_nlb, public;
ALTER TABLE kacho_nlb.nlb_outbox
    DROP CONSTRAINT nlb_outbox_resource_type_check,
    ADD CONSTRAINT nlb_outbox_resource_type_check
        CHECK (resource_type IN ('nlb_load_balancer', 'nlb_listener', 'nlb_target_group', 'notification'));

-- +goose Down
-- Обратный шаг сужает словарь. Если в журнале уже есть строки сигнала, постановка
-- узкого ограничения отказывает `23514` — это честный отказ, а не потеря строк.
SET search_path TO kacho_nlb, public;
ALTER TABLE kacho_nlb.nlb_outbox
    DROP CONSTRAINT nlb_outbox_resource_type_check,
    ADD CONSTRAINT nlb_outbox_resource_type_check
        CHECK (resource_type IN ('nlb_load_balancer', 'nlb_listener', 'nlb_target_group'));
