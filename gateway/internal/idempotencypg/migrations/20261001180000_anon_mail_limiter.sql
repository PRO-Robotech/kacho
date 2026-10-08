-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- 20261001180000 — хранилище ограничителя анонимной почты края (приёмка
-- NTF-2, sub-phase-NTF-2-identity-provider-mail-removal-acceptance.md, Р5,
-- NTF2-59…63, 74, 79; замысел issue-2917, З8, З9, З26, §6).
--
-- ───────────────────────────────────────────────────────────────────────────
-- ПОЧЕМУ В БАЗЕ ОДНОКРАТНОСТИ
--
-- Вид хранилища ограничителя — то же объявление, что у однократности
-- (`KACHO_IDEMPOTENCY_STORE`): `postgres` — состояние флота, видимое всем
-- репликам. Второго объявления «где живёт состояние края» не заводится. Пул у
-- ограничителя свой (CX2-44), база — та же.
--
-- ───────────────────────────────────────────────────────────────────────────
-- ЧЕМ ДЕРЖИТСЯ НЕДЕЛИМОСТЬ
--
-- Решение звена — одна транзакция: рекомендательные блокировки ключей (форма с
-- двумя int4) → пометка вызова (`pow_spent`, первичный ключ: вставка ON
-- CONFLICT DO NOTHING — повтор узнаёт оператор базы, а не пара «посмотреть —
-- записать», ban #10) → строка ведра FOR UPDATE последней → момент пропуска.
-- Строк-якорей у ключей моментов нет: сериализуется ключ, а не строка.
--
-- ───────────────────────────────────────────────────────────────────────────
-- ПРЕДМЕТЫ ХРАНЕНИЯ И ИХ УБОРКА (З26, CX2-35)
--
--   anon_mail_passes — уборщиком хранилища однократности сроком из функции
--                      З26 над таблицей границ края (верхняя граница окон + 1 ч);
--   pow_spent        — тем же уборщиком по expires_at (срок вызова);
--   anon_mail_bucket — одна строка на установку, уборке не подлежит.

-- +goose Up
CREATE TABLE IF NOT EXISTS kacho_gateway.anon_mail_passes (
    -- Ключ источника или подсети (`src/v4/…/32`, `net/v6/…/56` …).
    key         TEXT        NOT NULL,
    -- Момент пропуска по часам звена.
    at          TIMESTAMPTZ NOT NULL,
    -- Решение, записавшее строку: отличает свою фиксацию при разрешении
    -- исхода COMMIT (CX2-46).
    decision_id BYTEA       NOT NULL CHECK (octet_length(decision_id) = 16)
);

-- Счёт окна и чтение разрешения исхода — по (key, at).
CREATE INDEX IF NOT EXISTS anon_mail_passes_key_at_idx
    ON kacho_gateway.anon_mail_passes (key, at);
-- Уборка по сроку хранения.
CREATE INDEX IF NOT EXISTS anon_mail_passes_at_idx
    ON kacho_gateway.anon_mail_passes (at);

CREATE TABLE IF NOT EXISTS kacho_gateway.pow_spent (
    -- Идентификатор вызова: первичный ключ и есть механизм одноразовости.
    id         BYTEA       PRIMARY KEY CHECK (octet_length(id) = 16),
    -- Срок вызова: после него доказательство отвергает уже проверка срока.
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS pow_spent_expires_at_idx
    ON kacho_gateway.pow_spent (expires_at);

CREATE TABLE IF NOT EXISTS kacho_gateway.anon_mail_bucket (
    id     SMALLINT         PRIMARY KEY CHECK (id = 1),
    tokens DOUBLE PRECISION NOT NULL CHECK (tokens >= 0),
    at     TIMESTAMPTZ      NOT NULL
);

-- Строка-якорь ведра — обязана существовать у любой установки (не посев
-- стенда): момент в прошлом, и первый же расчёт наполняет ведро до BURST.
INSERT INTO kacho_gateway.anon_mail_bucket (id, tokens, at)
VALUES (1, 0, 'epoch')
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS kacho_gateway.anon_mail_bucket;
DROP TABLE IF EXISTS kacho_gateway.pow_spent;
DROP TABLE IF EXISTS kacho_gateway.anon_mail_passes;
