-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Курсор дельты величин называет ПРИЧИНУ молчания, а не показывает застой.
-- =============================================================================
-- Приёмка `docs/specs/sub-phase-KAN-QUOTA-1-limit-authority-leaves-iam-acceptance.md`,
-- стадия S1: производители П31 и П32, сценарии KAN-Q1-06 и KAN-Q4-13.
--
-- ЧЕГО НЕ БЫЛО. Накопительные счётчики заведены затем, чтобы тянущий, не
-- применивший ни строки, не выглядел здоровым: без них мёртвый синхронизатор
-- неотличим от живого на неизменной конфигурации. Этого достаточно, пока
-- авторитет величин существует.
--
-- Модуль квотирования уходит из службы доступа, и тогда «ни одна строка не
-- синхронизирована за всё время» становится штатным и ВЕЧНЫМ состоянием. Сигнал
-- застоя, оставленный как есть, срабатывает всегда — а проверку, кричащую на
-- нормальной работе, перестают читать вместе с настоящими находками. Причина
-- обязана называться, а не выводиться из нуля.
--
-- ПОЧЕМУ ОТДЕЛЬНЫЙ СТОЛБЕЦ, А НЕ ЗНАЧЕНИЕ В КУРСОРЕ. Курсор непрозрачен и
-- принадлежит владельцу величин; вписав в него своё слово, мы завели бы
-- значение, которое нельзя отличить от чужого содержимого. Плюс пустой курсор
-- уже означает «с начала времён» — третьего смысла у него быть не может.
--
-- НАПРАВЛЕНИЕ ОБРАТНОГО ЗАПОЛНЕНИЯ. Уже лежащая строка получает `unknown`, а не
-- `deployed`: на момент применения миграции ни один подъём ещё ничего не
-- объявлял, и записать за оператора «развёрнут» значило бы придумать намерение,
-- которого никто не выражал. Строка приходит к одному из двух законных значений
-- на первом же старте процесса.
--
-- СЛОВАРЬ ЗАКРЫТ ограничением, а не соглашением: три значения объявлены в
-- `pkg/quota` (AuthorityStates) и повторены здесь. Совпадение двух объявлений
-- держит интеграционная проба, а не совпадение написания.

-- +goose Up
-- +goose StatementBegin
SET search_path TO public, public;

ALTER TABLE public.quota_sync_cursor
    ADD COLUMN IF NOT EXISTS authority_state text NOT NULL DEFAULT 'unknown';

ALTER TABLE public.quota_sync_cursor
    DROP CONSTRAINT IF EXISTS quota_sync_cursor_authority_state_check;

ALTER TABLE public.quota_sync_cursor
    ADD CONSTRAINT quota_sync_cursor_authority_state_check
    CHECK (authority_state IN ('unknown', 'deployed', 'not-deployed'));

COMMENT ON COLUMN public.quota_sync_cursor.authority_state IS
    'what the operator declared to THIS consumer about the limit authority. '
    'Not decoration: once the authority is gone, "no row applied ever" becomes '
    'the normal and permanent state, so a zero counter stops meaning anything — '
    'the reason has to be named, not inferred. "unknown" means no process has '
    'declared anything yet';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO public, public;
ALTER TABLE public.quota_sync_cursor
    DROP CONSTRAINT IF EXISTS quota_sync_cursor_authority_state_check;
ALTER TABLE public.quota_sync_cursor DROP COLUMN IF EXISTS authority_state;
-- +goose StatementEnd
