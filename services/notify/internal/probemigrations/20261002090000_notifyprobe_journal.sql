-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- Схема пробы-источника notify-probe и её ЖУРНАЛ ПОДПИСКИ.
--
-- Проба — служба-источник извещений стенда: она ставит письмо в ленту и будит
-- notify строкой журнала по объекту notification_feed:notify-probe. Других видов
-- журнала у пробы нет: вид у неё один — ключ ленты `notification`, и строку
-- пишет функция фундамента (`subscription.Journal.Emit` через `feed.Put`) в
-- транзакции постановки. Ресурсов, чьё состояние журнал нёс бы, у пробы нет.
--
-- Журнал без проектного измерения: лента — предмет уровня кластера, якоря
-- проекта у её строки нет by construction (вид объявлен ScopeCluster).
--
-- Колонка инициатора заполняется умолчанием из настройки транзакции, которую
-- ставит `journaltx.Begin` (корневой транзакционный помощник журнала): строка,
-- записанная мимо помощника, не вставится — NOT NULL без значения.

CREATE SCHEMA IF NOT EXISTS kacho_notifyprobe;

CREATE TABLE kacho_notifyprobe.notifyprobe_outbox (
  -- Номер выдаёт счётчик на ВСТАВКЕ, а строка становится видимой на ФИКСАЦИИ:
  -- границу устоявшегося держит общий сервер потока, не журнал.
  sequence_no   BIGSERIAL    PRIMARY KEY,
  resource_kind TEXT         NOT NULL,
  resource_id   TEXT         NOT NULL,
  event_type    TEXT         NOT NULL,
  payload       JSONB        NOT NULL,
  initiator     TEXT         NOT NULL
                DEFAULT NULLIF(current_setting('kacho_journal.initiator', true), ''),
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  -- Словари ЗАКРЫТЫ: вид — один ключ ленты, род изменения — одно слово.
  -- Строка вне словаря недоставляема, и потеря эта тихая, поэтому она
  -- отсекается вставкой, а не обнаруживается подписчиком.
  CONSTRAINT notifyprobe_outbox_kind_check
    CHECK (resource_kind = 'notification'),
  CONSTRAINT notifyprobe_outbox_event_type_check
    CHECK (event_type = 'UPDATED'),
  CONSTRAINT notifyprobe_outbox_payload_object_check
    CHECK (jsonb_typeof(payload) = 'object')
);

-- Уборка журнала судит строки по возрасту (Retention = RetainsFromEarliestRow,
-- AgeColumn = created_at): индекс — ровно предикат уборщика.
CREATE INDEX notifyprobe_outbox_created_at_idx
  ON kacho_notifyprobe.notifyprobe_outbox (created_at);

-- +goose StatementBegin
-- Пробуждение подписки. Канал назван отдельно от таблицы: имя таблицы
-- схемо-квалифицировано, а `pg_notify` квалифицированного имени не принимает.
CREATE FUNCTION kacho_notifyprobe.notifyprobe_outbox_notify() RETURNS trigger
  LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('notifyprobe_outbox', NEW.sequence_no::text);
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER notifyprobe_outbox_notify_trg
  AFTER INSERT ON kacho_notifyprobe.notifyprobe_outbox
  FOR EACH ROW EXECUTE FUNCTION kacho_notifyprobe.notifyprobe_outbox_notify();

-- +goose Down
-- Возврат в форму без журнала. Позиции подписчиков теряются вместе с таблицей:
-- notify, возобновляющийся с сохранённой позиции, получит отказ «позиция больше
-- не возобновима» и начнёт с начала; строки ленты от этого не теряются — их
-- забирает Claim, а не поток.
DROP TRIGGER IF EXISTS notifyprobe_outbox_notify_trg ON kacho_notifyprobe.notifyprobe_outbox;
DROP FUNCTION IF EXISTS kacho_notifyprobe.notifyprobe_outbox_notify();
DROP TABLE IF EXISTS kacho_notifyprobe.notifyprobe_outbox;
DROP SCHEMA IF EXISTS kacho_notifyprobe;
