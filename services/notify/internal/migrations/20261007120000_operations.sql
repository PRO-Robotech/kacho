-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- operations — длительные операции службы notify в базе kacho_notify (kacho#2924;
-- приёмка sub-phase-NTF-5-operator-notices-acceptance.md редакции 20, Р2, Р23;
-- замысел issue-2924 З2, З3, З17).
--
-- Строку пишет только хранилище фундамента (corelib operations): Ф1 — приём
-- `InternalNoticeService.Create` (done = false), Ф2 — переходы и `Update`
-- (рождённая завершённой), Ф3/Ф4 — фиксация и терминальный отказ заявки
-- в notify-sender. Своего SQL к этой таблице у notify нет (Р23).
--
-- Набор столбцов — тот, что вставляет и читает хранилище фундамента
-- (`insertOperationTx`, `GetOwned`, `CancelOwned`): `account_id` пишется
-- вставкой безусловно, у операций notify он NULL (метаданные извещения поля
-- account_id не несут). Принципал — без умолчания: Ф1 и Ф2 отказывают
-- анонимному до записи, запасного системного принципала у notify нет (З17), и
-- строка без принципала — нарушение, а не «системная операция».
--
-- Индексов, кроме первичного ключа, нет: читатели таблицы в notify — опрос и
-- отмена операции по id (`OperationService.Get`/`Cancel`) и чтение `done` по
-- id в notify-sender (`ops.Get`); списка операций notify не служит, движка
-- `Reconciler` не поднимает (З4). Индекс вперёд читателя — обещание, которого
-- никто не держит.

CREATE TABLE operations (
  id                     text        NOT NULL,
  description            text        NOT NULL,
  created_at             timestamptz NOT NULL,
  created_by             text        NOT NULL,
  modified_at            timestamptz NOT NULL,
  done                   boolean     NOT NULL,
  metadata_type          text        NULL,
  metadata_data          bytea       NULL,
  resource_id            text        NULL,
  account_id             text        NULL,
  error_code             integer     NULL,
  error_message          text        NULL,
  error_details          bytea       NULL,
  response_type          text        NULL,
  response_data          bytea       NULL,
  principal_type         text        NOT NULL,
  principal_id           text        NOT NULL,
  principal_display_name text        NOT NULL,
  CONSTRAINT operations_pkey PRIMARY KEY (id),
  CONSTRAINT operations_id_chk CHECK (id <> ''),
  -- Владелец операции называет кого-то: пустой тип или id — анонимная строка,
  -- которую GetOwned/CancelOwned не отдали бы никому.
  CONSTRAINT operations_principal_chk CHECK (principal_type <> '' AND principal_id <> '')
);

-- +goose Down
DROP TABLE IF EXISTS operations;
