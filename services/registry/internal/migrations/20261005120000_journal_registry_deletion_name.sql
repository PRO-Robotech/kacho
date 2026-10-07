-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Снятие реестра несёт СНИМОК ИМЕНИ.
-- =============================================================================
-- Задача `PRO-Robotech/kacho#2918` (полоса S1-A3); приёмка
-- `docs/specs/sub-phase-NTF-3-kacho-modules-notifications-acceptance.md`, решение
-- Р2, сценарий NTF3-59 (снятие несёт снимок имени) и заказ УК3-13 (б) (близнец
-- `DELETED Registry` → имя).
--
-- Вид `Registry` журнала объявлен формой имени DNS-метки (`NameFormDNS`,
-- `internal/subscriptionjournal`): сервер потока отдаёт на событии снятия имя из
-- нагрузки строки под ключом `name`. Прежнее тело функции клало в нагрузку снятия
-- один идентификатор — событие уезжало без имени, а сервер писал жалобу.
--
-- Снимок берётся из строки `OLD` — той, которую удалил оператор, вызвавший
-- триггер, — а не чтением до удаления: переименование, зафиксированное между
-- чтением и удалением, дало бы снятию чужое имя.
--
-- Состояния у снятия по-прежнему нет: имя — оболочка события, а не состояние
-- предмета, и читать нагрузку снятия как полное состояние подписчик не вправе.
--
-- Колонку `initiator` функция не называет: её значение даёт умолчание колонки из
-- настройки транзакции (миграция `20261004170000_journal_initiator.sql`).

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_registry.registries_journal_emit() RETURNS trigger
  LANGUAGE plpgsql AS $$
DECLARE
  v_event   text;
  v_row     record;
  v_payload jsonb;
BEGIN
  IF TG_OP = 'DELETE' THEN
    v_event := 'DELETED';
    v_row   := OLD;
  ELSIF TG_OP = 'UPDATE' THEN
    v_event := 'UPDATED';
    v_row   := NEW;
  ELSE
    v_event := 'CREATED';
    v_row   := NEW;
  END IF;

  IF v_event = 'DELETED' THEN
    v_payload := jsonb_build_object('id', v_row.id, 'name', v_row.name);
  ELSE
    v_payload := to_jsonb(v_row) || jsonb_build_object(
      'created_at',
      to_char(v_row.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'));
  END IF;

  INSERT INTO kacho_registry.registry_resource_journal
    (resource_kind, resource_id, project_id, event_type, payload)
  VALUES ('Registry', v_row.id, v_row.project_id, v_event, v_payload);

  RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Возврат к телу без снимка имени: снятие снова уезжает одним идентификатором.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_registry.registries_journal_emit() RETURNS trigger
  LANGUAGE plpgsql AS $$
DECLARE
  v_event   text;
  v_row     record;
  v_payload jsonb;
BEGIN
  IF TG_OP = 'DELETE' THEN
    v_event := 'DELETED';
    v_row   := OLD;
  ELSIF TG_OP = 'UPDATE' THEN
    v_event := 'UPDATED';
    v_row   := NEW;
  ELSE
    v_event := 'CREATED';
    v_row   := NEW;
  END IF;

  IF v_event = 'DELETED' THEN
    v_payload := jsonb_build_object('id', v_row.id);
  ELSE
    v_payload := to_jsonb(v_row) || jsonb_build_object(
      'created_at',
      to_char(v_row.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'));
  END IF;

  INSERT INTO kacho_registry.registry_resource_journal
    (resource_kind, resource_id, project_id, event_type, payload)
  VALUES ('Registry', v_row.id, v_row.project_id, v_event, v_payload);

  RETURN NULL;
END;
$$;
-- +goose StatementEnd
