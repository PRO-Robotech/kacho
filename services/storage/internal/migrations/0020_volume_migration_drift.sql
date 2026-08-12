-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0020: смена класса тома видна сверщику.
--
-- WHY. Смене класса НЕ заводится собственное состояние строки, и это не экономия, а
-- следование той же линии, что уже принята в этом сервисе: состояние выводится, а не
-- хранится. Привязанность тома выводится из наличия строки привязки — и переезд
-- выводится из расхождения РЕВИЗИЙ: желаемая назначена, действующая ещё прежняя.
-- Хранить это третьим значением колонки значило бы завести второй источник истины об
-- одном факте, а они расходятся — вопрос лишь когда.
--
-- Отсюда единственное, что нужно от схемы: расхождение ревизий обязано попадать в
-- рабочий список сверщика. Прежний частичный индекс ловил только расхождение
-- намерения с наблюдаемым, и переезд остался бы невидим — том навсегда завис бы
-- между классами, а сверщик исправно рапортовал бы, что расхождений нет.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DROP INDEX IF EXISTS kacho_storage.volumes_drift_idx;

CREATE INDEX volumes_drift_idx
    ON kacho_storage.volumes (updated_at)
 WHERE state <> observed_state
    OR (desired_binding_id IS NOT NULL AND desired_binding_id IS DISTINCT FROM binding_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DROP INDEX IF EXISTS kacho_storage.volumes_drift_idx;

CREATE INDEX volumes_drift_idx
    ON kacho_storage.volumes (updated_at) WHERE state <> observed_state;
-- +goose StatementEnd
