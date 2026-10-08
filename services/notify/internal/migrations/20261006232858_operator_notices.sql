-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- Извещения оператора, стадия S1 (kacho#2924; приёмка
-- sub-phase-NTF-5-operator-notices-acceptance.md редакции 20, Р4, Р5, Р8, Р10, Р14,
-- Р19; замысел issue-2924 §6, З3, З5 п.5, З6, З7, З9 п.1 и п.4, З21, З27):
-- заявка создания, извещение, его аудитория, затронутые ресурсы, напоминания,
-- события этапа и счётчики исходов.
--
-- Инварианты — уровня базы (ban #10). Каждое ограничение названо: единственный
-- дом отображения SQLSTATE службы переводит `23514` в закрытый текст Р4 по имени
-- ограничения (З5 п.5), поэтому имя — часть контракта.
--
-- Отсутствие — одним написанием (замысел §6): аренда, позиция раскрытия, признак
-- «раскрытие завершено», моменты переходов — `NULL`; пустая строка там, где
-- отсутствие законно, закрыта `CHECK`.
--
-- Чего здесь нет и почему:
--   * кэша областей — приёмка редакции 20 его снимает (Р10 «Кэша областей нет»,
--     Д131): аккаунт проекта записан в аудиторию фиксацией, владельца отдаёт
--     справочник адресов на момент отправки;
--   * снимка аудитории у события этапа — извещение не событие ресурса, адресата
--     даёт только цепочка контактов и владелец (Р8, Д129);
--   * строк адресатов, кандидатов, исходов областей и окна OB — стадия S2,
--     отдельная миграция.

-- notice_create_requests — заявка `Create` до проверки областей у владельца
-- (Р10 шаги 1–6, З3). Пишет её `notify-api` вместе с операцией, снимает
-- `notify-sender` той же транзакцией, что завершает операцию. Поля — присланные
-- оператором, в порядке запроса: первая несуществующая область называется в
-- ответе (шаг 4), поэтому списки — упорядоченные массивы.
CREATE TABLE notice_create_requests (
  notice_id            text        NOT NULL,
  operation_id         text        NOT NULL,
  kind                 text        NOT NULL,
  starts_at            timestamptz NULL,
  ends_at              timestamptz NULL,
  audience_all         boolean     NOT NULL,
  audience_account_ids text[]      NOT NULL DEFAULT '{}',
  audience_project_ids text[]      NOT NULL DEFAULT '{}',
  affected_types       text[]      NOT NULL DEFAULT '{}',
  affected_ids         text[]      NOT NULL DEFAULT '{}',
  created_by           text        NOT NULL,
  accepted_at          timestamptz NOT NULL,
  -- Аренда взятия (З27): вставка без аренды, «свободна» —
  -- (lease_until IS NULL OR lease_until <= $now) по часам notify.
  lease_token          uuid        NULL,
  lease_until          timestamptz NULL,
  CONSTRAINT notice_create_requests_pkey PRIMARY KEY (notice_id),
  CONSTRAINT notice_create_requests_operation_uniq UNIQUE (operation_id),
  CONSTRAINT notice_create_requests_ids_chk
    CHECK (notice_id <> '' AND operation_id <> ''),
  CONSTRAINT notice_create_requests_created_by_chk CHECK (created_by <> ''),
  CONSTRAINT notice_create_requests_kind_chk
    CHECK (kind IN ('MAINTENANCE', 'OUTAGE', 'DECOMMISSION', 'SUSPENSION',
                    'SECURITY_INCIDENT', 'TERMS_CHANGE')),
  -- Р4: startsAt обязателен у видов, рождённых в SCHEDULED, и запрещён у прочих
  -- (их момент ставит сервер); endsAt — только у MAINTENANCE и там обязателен.
  CONSTRAINT notice_create_requests_starts_at_kind_chk
    CHECK ((kind IN ('MAINTENANCE', 'DECOMMISSION', 'TERMS_CHANGE')) = (starts_at IS NOT NULL)),
  CONSTRAINT notice_create_requests_ends_at_kind_chk
    CHECK ((kind = 'MAINTENANCE') = (ends_at IS NOT NULL)),
  CONSTRAINT notice_create_requests_ends_after_starts_chk
    CHECK (ends_at IS NULL OR ends_at > starts_at),
  -- Р10: ровно одна форма аудитории; «все аккаунты» запрещено приостановке.
  CONSTRAINT notice_create_requests_audience_one_form_chk
    CHECK (audience_all::int
           + (cardinality(audience_account_ids) > 0)::int
           + (cardinality(audience_project_ids) > 0)::int = 1),
  CONSTRAINT notice_create_requests_audience_all_kind_chk
    CHECK (NOT (audience_all AND kind = 'SUSPENSION')),
  CONSTRAINT notice_create_requests_audience_items_chk
    CHECK (cardinality(audience_account_ids) <= 100
           AND cardinality(audience_project_ids) <= 100
           AND coalesce(array_ndims(audience_account_ids), 1) = 1
           AND coalesce(array_ndims(audience_project_ids), 1) = 1
           AND array_position(audience_account_ids, NULL) IS NULL
           AND array_position(audience_account_ids, '') IS NULL
           AND array_position(audience_project_ids, NULL) IS NULL
           AND array_position(audience_project_ids, '') IS NULL),
  -- Р4: не больше 50 ссылок {type, id}, тип — из закрытого перечня 18 типов
  -- ресурса арендатора, ссылки — только у MAINTENANCE, OUTAGE, DECOMMISSION.
  CONSTRAINT notice_create_requests_affected_chk
    CHECK (cardinality(affected_types) = cardinality(affected_ids)
           AND cardinality(affected_types) <= 50
           AND coalesce(array_ndims(affected_types), 1) = 1
           AND coalesce(array_ndims(affected_ids), 1) = 1
           AND array_position(affected_types, NULL) IS NULL
           AND array_position(affected_ids, NULL) IS NULL
           AND array_position(affected_ids, '') IS NULL
           AND affected_types <@ ARRAY[
             'vpc_network', 'vpc_subnet', 'vpc_security_group', 'vpc_route_table',
             'vpc_address', 'vpc_gateway', 'vpc_network_interface', 'vpc_cidr_group',
             'compute_instance', 'compute_placement_group',
             'nlb_network_load_balancer', 'nlb_target_group', 'nlb_listener',
             'registry_registry', 'registry_repository',
             'storage_volume', 'storage_snapshot', 'storage_image']::text[]
           AND (cardinality(affected_types) = 0
                OR kind IN ('MAINTENANCE', 'OUTAGE', 'DECOMMISSION'))),
  CONSTRAINT notice_create_requests_lease_pair_chk
    CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);

-- Взятие заявки: ORDER BY accepted_at, notice_id … FOR UPDATE SKIP LOCKED (З3 п.2).
CREATE INDEX notice_create_requests_pick_idx ON notice_create_requests (accepted_at, notice_id);

-- notices — извещение (Р4, Р5). Моменты — секунды (З25); startsAt есть у
-- каждого вида: у рождённых в IN_PROGRESS его ставит сервер моментом приёма.
CREATE TABLE notices (
  id           text        NOT NULL,
  kind         text        NOT NULL,
  state        text        NOT NULL,
  starts_at    timestamptz NOT NULL,
  ends_at      timestamptz NULL,
  audience_all boolean     NOT NULL,
  revision     integer     NOT NULL DEFAULT 1,
  created_by   text        NOT NULL,
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  -- Незаполненный момент — «такого перехода не было», и только это (Р5).
  started_at   timestamptz NULL,
  completed_at timestamptz NULL,
  cancelled_at timestamptz NULL,
  CONSTRAINT notices_pkey PRIMARY KEY (id),
  -- Цель составного внешнего ключа затронутых ресурсов: вид неизменяем, и
  -- допустимость ссылок по виду держит ключ, а не чтение вида кодом.
  CONSTRAINT notices_id_kind_uniq UNIQUE (id, kind),
  CONSTRAINT notices_id_chk CHECK (id <> ''),
  CONSTRAINT notices_created_by_chk CHECK (created_by <> ''),
  CONSTRAINT notices_kind_chk
    CHECK (kind IN ('MAINTENANCE', 'OUTAGE', 'DECOMMISSION', 'SUSPENSION',
                    'SECURITY_INCIDENT', 'TERMS_CHANGE')),
  CONSTRAINT notices_state_chk
    CHECK (state IN ('SCHEDULED', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED')),
  -- Р4: endsAt — только у MAINTENANCE и там обязателен.
  CONSTRAINT notices_ends_at_kind_chk
    CHECK ((kind = 'MAINTENANCE') = (ends_at IS NOT NULL)),
  CONSTRAINT notices_ends_after_starts_chk
    CHECK (ends_at IS NULL OR ends_at > starts_at),
  -- Р4: у вида, рождённого в SCHEDULED, startsAt строго позже приёма (проверка
  -- `> now` при приёме и при Update — часами notify, не позже created_at).
  CONSTRAINT notices_starts_after_created_chk
    CHECK (kind NOT IN ('MAINTENANCE', 'DECOMMISSION', 'TERMS_CHANGE')
           OR starts_at > created_at),
  -- Р4, Р5: вид, рождённый в IN_PROGRESS, не бывает SCHEDULED и CANCELLED; его
  -- startsAt и startedAt — момент приёма заявки.
  CONSTRAINT notices_born_in_progress_chk
    CHECK (kind NOT IN ('OUTAGE', 'SUSPENSION', 'SECURITY_INCIDENT')
           OR (state NOT IN ('SCHEDULED', 'CANCELLED')
               AND starts_at = created_at
               AND started_at = created_at)),
  -- Р5: момент перехода задан тогда и только тогда, когда переход был.
  CONSTRAINT notices_state_moments_chk
    CHECK ((started_at IS NOT NULL) = (state IN ('IN_PROGRESS', 'COMPLETED'))
           AND (completed_at IS NOT NULL) = (state = 'COMPLETED')
           AND (cancelled_at IS NOT NULL) = (state = 'CANCELLED')),
  CONSTRAINT notices_moments_order_chk
    CHECK (updated_at >= created_at
           AND (started_at IS NULL OR started_at >= created_at)
           AND (completed_at IS NULL OR completed_at >= started_at)
           AND (cancelled_at IS NULL OR cancelled_at >= created_at)),
  -- Р5: ревизию повышает только Update, а он есть у трёх видов.
  CONSTRAINT notices_revision_chk
    CHECK (revision >= 1
           AND (revision = 1 OR kind IN ('MAINTENANCE', 'DECOMMISSION', 'TERMS_CHANGE'))),
  CONSTRAINT notices_audience_all_kind_chk
    CHECK (NOT (audience_all AND kind = 'SUSPENSION'))
);

-- Список и курсор (created_at, id) — публичное и внутреннее чтение (Р16).
CREATE INDEX notices_created_idx ON notices (created_at, id);

-- notice_audience — области аудитории в порядке запроса (Р10). Порядковый номер
-- — позиция раскрытия областей: scope_done события этапа — номер последней
-- завершённой области (З9 п.1). Аккаунт проекта записан фиксацией из ответа
-- владельца (пара «проект → аккаунт» неизменяема); у области-аккаунта — она сама.
CREATE TABLE notice_audience (
  notice_id  text     NOT NULL,
  ordinal    smallint NOT NULL,
  scope_type text     NOT NULL,
  scope_id   text     NOT NULL,
  account_id text     NOT NULL,
  CONSTRAINT notice_audience_pkey PRIMARY KEY (notice_id, scope_type, scope_id),
  CONSTRAINT notice_audience_ordinal_uniq UNIQUE (notice_id, ordinal),
  CONSTRAINT notice_audience_notice_fk FOREIGN KEY (notice_id)
    REFERENCES notices (id) ON DELETE CASCADE,
  CONSTRAINT notice_audience_ordinal_chk CHECK (ordinal >= 0 AND ordinal < 100),
  CONSTRAINT notice_audience_scope_type_chk CHECK (scope_type IN ('account', 'project')),
  CONSTRAINT notice_audience_ids_chk CHECK (scope_id <> '' AND account_id <> ''),
  CONSTRAINT notice_audience_account_scope_chk
    CHECK (scope_type <> 'account' OR account_id = scope_id)
);

CREATE INDEX notice_audience_scope_idx ON notice_audience (scope_type, scope_id);
CREATE INDEX notice_audience_account_idx ON notice_audience (account_id);

-- Форма аудитории против признака «все аккаунты» (Р10, замысел §6): при
-- audience_all строк нет; иначе строки есть, все одной формы (аккаунты либо
-- проекты), номера идут подряд с нуля. Предмет — две таблицы, поэтому это
-- отложенный до фиксации триггер-ограничение, а не CHECK. Строку удалённого
-- извещения (каскад подметальщика) он не судит.
-- +goose StatementBegin
CREATE FUNCTION notice_audience_form_check() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  ids     text[];
  nid     text;
  all_set boolean;
  n_rows  integer;
  n_forms integer;
  max_ord integer;
BEGIN
  IF TG_TABLE_NAME = 'notices' THEN
    ids := ARRAY[NEW.id];
  ELSIF TG_OP = 'INSERT' THEN
    ids := ARRAY[NEW.notice_id];
  ELSIF TG_OP = 'DELETE' THEN
    ids := ARRAY[OLD.notice_id];
  ELSE
    ids := ARRAY[OLD.notice_id, NEW.notice_id];
  END IF;

  FOREACH nid IN ARRAY ids LOOP
    SELECT audience_all INTO all_set FROM notices WHERE id = nid;
    CONTINUE WHEN NOT FOUND;

    SELECT count(*), count(DISTINCT scope_type), max(ordinal)
      INTO n_rows, n_forms, max_ord
      FROM notice_audience WHERE notice_id = nid;

    IF (all_set AND n_rows <> 0)
       OR (NOT all_set AND (n_rows = 0 OR n_forms <> 1 OR max_ord <> n_rows - 1)) THEN
      RAISE EXCEPTION 'notice % audience does not match its form', nid
        USING ERRCODE = 'check_violation',
              CONSTRAINT = 'notices_audience_form_chk',
              TABLE = 'notices';
    END IF;
  END LOOP;
  RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER notices_audience_form_trg
  AFTER INSERT OR UPDATE OF audience_all ON notices
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION notice_audience_form_check();

CREATE CONSTRAINT TRIGGER notice_audience_form_trg
  AFTER INSERT OR UPDATE OR DELETE ON notice_audience
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION notice_audience_form_check();

-- notice_affected_resources — затронутые ресурсы в порядке запроса (Р4).
-- Вид извещения — часть внешнего ключа: ссылка у вида вне трёх разрешённых
-- невыразима, и вид ссылки не расходится с видом извещения.
CREATE TABLE notice_affected_resources (
  notice_id     text     NOT NULL,
  notice_kind   text     NOT NULL,
  ordinal       smallint NOT NULL,
  resource_type text     NOT NULL,
  resource_id   text     NOT NULL,
  CONSTRAINT notice_affected_resources_pkey PRIMARY KEY (notice_id, ordinal),
  CONSTRAINT notice_affected_resources_notice_fk FOREIGN KEY (notice_id, notice_kind)
    REFERENCES notices (id, kind) ON DELETE CASCADE,
  CONSTRAINT notice_affected_resources_kind_chk
    CHECK (notice_kind IN ('MAINTENANCE', 'OUTAGE', 'DECOMMISSION')),
  CONSTRAINT notice_affected_resources_ordinal_chk CHECK (ordinal >= 0 AND ordinal < 50),
  CONSTRAINT notice_affected_resources_id_chk CHECK (resource_id <> ''),
  CONSTRAINT notice_affected_resources_type_chk
    CHECK (resource_type IN (
      'vpc_network', 'vpc_subnet', 'vpc_security_group', 'vpc_route_table',
      'vpc_address', 'vpc_gateway', 'vpc_network_interface', 'vpc_cidr_group',
      'compute_instance', 'compute_placement_group',
      'nlb_network_load_balancer', 'nlb_target_group', 'nlb_listener',
      'registry_registry', 'registry_repository',
      'storage_volume', 'storage_snapshot', 'storage_image'))
);

-- notice_reminders — назначенные напоминания (Р5, З7): одна строка на момент;
-- планировщик снимает строку записью DELETE … RETURNING и только тогда пишет
-- событие reminder.
CREATE TABLE notice_reminders (
  notice_id text        NOT NULL,
  at        timestamptz NOT NULL,
  revision  integer     NOT NULL,
  CONSTRAINT notice_reminders_pkey PRIMARY KEY (notice_id, at),
  CONSTRAINT notice_reminders_notice_fk FOREIGN KEY (notice_id)
    REFERENCES notices (id) ON DELETE CASCADE,
  CONSTRAINT notice_reminders_revision_chk CHECK (revision >= 1)
);

-- Планировщик берёт наступившие моменты.
CREATE INDEX notice_reminders_at_idx ON notice_reminders (at);

-- notice_stage_events — событие этапа (stage, revision, dueAt) (Р8, З6, З9).
-- closed_at ставит только переход (З6 п.2); fanned_out_at — только последняя
-- страница либо последняя область раскрытия (З9 п.4). Позиция раскрытия — одна
-- из двух форм, начальная выражена отдельно от любой законной: scope_done IS
-- NULL — «ни одна область не завершена», page_token IS NULL — «страниц не было»
-- (З9 п.1).
CREATE TABLE notice_stage_events (
  id            uuid        NOT NULL DEFAULT gen_random_uuid(),
  notice_id     text        NOT NULL,
  stage         text        NOT NULL,
  revision      integer     NOT NULL,
  due_at        timestamptz NOT NULL,
  closed_at     timestamptz NULL,
  fanned_out_at timestamptz NULL,
  scope_done    integer     NULL,
  page_token    text        NULL,
  -- Аренда раскрытия (З27).
  lease_token   uuid        NULL,
  lease_until   timestamptz NULL,
  CONSTRAINT notice_stage_events_pkey PRIMARY KEY (id),
  CONSTRAINT notice_stage_events_event_uniq UNIQUE (notice_id, stage, revision, due_at),
  CONSTRAINT notice_stage_events_notice_fk FOREIGN KEY (notice_id)
    REFERENCES notices (id) ON DELETE CASCADE,
  CONSTRAINT notice_stage_events_stage_chk
    CHECK (stage IN ('scheduled', 'reminder', 'rescheduled', 'cancelled', 'completed',
                     'started', 'resolved', 'lifted', 'opened', 'closed')),
  CONSTRAINT notice_stage_events_revision_chk CHECK (revision >= 1),
  CONSTRAINT notice_stage_events_scope_done_chk CHECK (scope_done IS NULL OR scope_done >= 0),
  CONSTRAINT notice_stage_events_page_token_chk CHECK (page_token IS NULL OR page_token <> ''),
  CONSTRAINT notice_stage_events_position_chk CHECK (scope_done IS NULL OR page_token IS NULL),
  CONSTRAINT notice_stage_events_lease_pair_chk
    CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);

-- Взятие события раскрытием и метрика notify_notice_fanout_pending (З9 п.4).
CREATE INDEX notice_stage_events_pending_idx ON notice_stage_events (due_at)
  WHERE closed_at IS NULL AND fanned_out_at IS NULL;

-- notice_counters — счётчики исходов Р19 в базе (З21 п.1): приращение — в той же
-- транзакции, что пишет исход, одним оператором в порядке (name, labels)
-- последним. labels — каноническая запись набора ярлыков; '' — набор пуст.
CREATE TABLE notice_counters (
  name   text   NOT NULL,
  labels text   NOT NULL,
  n      bigint NOT NULL,
  CONSTRAINT notice_counters_pkey PRIMARY KEY (name, labels),
  CONSTRAINT notice_counters_name_chk
    CHECK (name IN ('notify_notice_letters_total', 'notify_notice_unaddressed_total',
                    'notify_ob_deferred_total', 'notify_notice_observer_orphan_total')),
  CONSTRAINT notice_counters_n_chk CHECK (n >= 0)
);

-- +goose Down
DROP TABLE IF EXISTS notice_counters;
DROP TABLE IF EXISTS notice_stage_events;
DROP TABLE IF EXISTS notice_reminders;
DROP TABLE IF EXISTS notice_affected_resources;
DROP TABLE IF EXISTS notice_audience;
DROP TABLE IF EXISTS notices;
DROP FUNCTION IF EXISTS notice_audience_form_check();
DROP TABLE IF EXISTS notice_create_requests;
