-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- Схема шлюза notify (база kacho_notify): сетка на адресата, суточный потолок
-- потока и ограда ключа сетки (kacho#2915, замысел NTF-1 З24 и §6, NTF1-H01…H10).
--
-- Инварианты — уровня базы (ban #10): счётчик не ниже нуля, строка ограды одна,
-- отпечаток ровно 16 байт. Резерв — один CAS
-- `INSERT … ON CONFLICT … DO UPDATE … WHERE count < $limit`, отдельного «есть ли
-- место» нет; его пишет services/notify/internal/limits.
--
-- Адреса открытым текстом схема не несёт (NTF1-H03): ключ строки сетки —
-- HMAC-SHA256(ключ сетки, нормализованный адрес), а ограда хранит отпечаток
-- ключа, а не ключ.

-- recipient_net — строка счётчика на (ключ адресата, класс, окно, начало окна).
-- Окно — `period` ('hour' | 'day'): имя `window` в Postgres зарезервировано.
CREATE TABLE recipient_net (
  key          bytea       NOT NULL CHECK (octet_length(key) = 32),
  class        text        NOT NULL CHECK (class IN ('security', 'notice')),
  period       text        NOT NULL CHECK (period IN ('hour', 'day')),
  window_start timestamptz NOT NULL,
  count        integer     NOT NULL CHECK (count >= 0),
  PRIMARY KEY (key, class, period, window_start)
);

-- global_daily — суточный потолок потока, одна строка на сутки UTC.
CREATE TABLE global_daily (
  day   date    PRIMARY KEY,
  count integer NOT NULL CHECK (count >= 0)
);

-- recipient_key_fence — отпечаток действующего ключа сетки, строка-одиночка.
-- Пишет её старт реплики под LOCK TABLE … IN EXCLUSIVE MODE, читает FOR SHARE
-- первым оператором каждая транзакция резерва (З24, CX1-66, CX1-68, CX1-69).
CREATE TABLE recipient_key_fence (
  singleton   boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  fingerprint bytea   NOT NULL CHECK (octet_length(fingerprint) = 16)
);

-- +goose Down
DROP TABLE IF EXISTS recipient_key_fence;
DROP TABLE IF EXISTS global_daily;
DROP TABLE IF EXISTS recipient_net;
