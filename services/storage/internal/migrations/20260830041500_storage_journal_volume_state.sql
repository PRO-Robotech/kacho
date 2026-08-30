-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin
-- Состояние ПРЕДМЕТА в журнале подписки — у вида «том».
--
-- Задача `PRO-Robotech/kacho#1552`.
--
-- ЗАЧЕМ. Решение о единой форме подписки отдало клиенту отбор по меткам НА ТОМ
-- ОСНОВАНИИ, что событие несёт полное состояние ресурса. У storage основание не
-- выполнялось ни у одного вида: событие несло оболочку, и клиентский отбор по
-- меткам был для его видов НЕИСПОЛНИМ. Здесь основание вводится для тома.
--
-- Предыдущая миграция журнала НЕ правится (ban #5): функции заменяются
-- `CREATE OR REPLACE`, таблица и триггеры остаются те же.
--
--
-- ПОЧЕМУ ЭТО НЕ ВТОРАЯ РЕАЛИЗАЦИЯ ПУБЛИЧНОЙ ПРОЕКЦИИ — довод, снявший прежнее решение
--
-- Прежнее решение («состояния не будет») стояло на том, что публичная проекция
-- тома выводится ЧЕРЕЗ таблицы, и собрать её в триггере значило бы записать вторую
-- реализацию `internal/protoconv` на SQL — она разошлась бы с Go молча.
--
-- Довод верен и здесь НЕ нарушен: SQL не вычисляет ни одного производного поля.
-- Он кладёт в конверт ровно те ВХОДЫ, что подаёт запрос чтения своим
-- `LEFT JOIN volume_attachments`, — строку тома и её привязки. Статус по-прежнему
-- выводит единственная `domain.DeriveStatus`, проекцию собирает единственный
-- `protoconv.Volume`. На SQL переехал способ ДОБЫТЬ входы, и он тот же, что у
-- чтения; проекция осталась в Go, в одном месте.
--
-- Ровно это и назвал предикат пересмотра прежнего решения: состояние вводится,
-- когда публичную проекцию можно получить, НЕ переписывая её. Предикат сработал.
--
--
-- ПОЧЕМУ ТОЛЬКО ТОМ — и почему у снимка с образом причина ДРУГАЯ
--
-- Не «у них тоже выводится через таблицы»: входы нашлись бы и у них. У этих входов
-- нет ПРОИЗВОДИТЕЛЯ СОБЫТИЙ. `used_by` снимка и образа — перечень ТОМОВ, засеянных
-- этим источником (`volumes.source_snapshot_id` / `source_image_id`), а мутация
-- ТОМА события снимку и образу не эмитирует ни один триггер. Отдай мы им
-- состояние, оно устаревало бы МОЛЧА: подписчик держал бы снимок с пустым
-- `used_by` при живых детях и получил бы отказ удаления, которого его собственное
-- состояние не объясняет — то есть ровно ту ложь, ради которой состояние и
-- вводится.
--
-- У тома оба перекрёстных входа лежат в `volume_attachments`, и у неё СВОЙ триггер
-- эмиссии (`volume_attachments_storage_outbox_emit`, предыдущая миграция).
-- Поэтому его состояние свежо ПО ПОСТРОЕНИЮ: изменился любой вход — событие есть.
--
-- Заводить состояние снимку и образу — значит СПЕРВА завести производителя их
-- событий (мутация тома эмитировала бы до двух лишних событий соседним видам).
-- Это отдельный предмет со своей ценой, и он заведён задачей продукта #1556, а
-- не назван здесь исполненным.
--
--
-- ПОЧЕМУ У СНЯТИЯ КОНВЕРТА НЕТ
--
-- Предмета больше нет: собирать было нечего, попытки не было. Строка снятия
-- остаётся прежней, минимальной формы, и сборщик отвечает по ней «не
-- производится» — не ошибкой.
--
--
-- ПОЧЕМУ КЛЮЧ КОНВЕРТА С ТОЧКОЙ
--
-- Журнал НЕ чистится, и подписчик вправе открыть поток с начала — значит строки,
-- записанные ДО этой миграции, доезжают до сборщика и сегодня. Отличить их надо ПО
-- ПОСТРОЕНИЮ, а не по удаче разбора: прежняя нагрузка — та же строка `volumes`,
-- она разобралась бы БЕЗ ОТКАЗА и дала бы привязанный том как доступный и без
-- единой привязки.
--
-- Ключи прежней нагрузки — ИМЕНА КОЛОНОК (`to_jsonb` строки). Значит конвертом
-- может служить только написание, которое именем колонки БЫТЬ НЕ МОЖЕТ.
-- Незакавыченный идентификатор Postgres состоит из букв, цифр, `_` и `$`; точки в
-- нём нет, а закавыченных имён в этой схеме нет ни одного. Поэтому `to_jsonb`
-- любой строки любой её таблицы такого ключа не произведёт НИКОГДА — ни сегодня,
-- ни после колонки, которую заведут завтра.
--
-- Цена измерена, а не предположена: первая редакция взяла у соседнего владельца
-- имя `state`, одноимённое КОЛОНКЕ тома. Строка прежней формы несла
-- `"state":"READY"`, разборщик доставал под этим ключом строку вместо объекта и
-- отказывал — событие уезжало причиной «собрать не удалось», то есть звало
-- перечитывать вечно то, чего никто не терял.
--
--
-- ЧЕГО В КОНВЕРТЕ НЕТ И ПОЧЕМУ
--
-- Привязки перечисляются ПОИМЁННЫМ списком колонок, а не `to_jsonb(a)`. Довод —
-- тот же, по которому из нагрузки исключены инфра-колонки тома: журнал кормит
-- ручку, доступную арендатору, и колонка, заведённая на этой таблице завтра,
-- уехала бы в конверт, не будучи ни разу рассмотрена. Поимённый список делает
-- расширение решением, а не побочным эффектом.
--
-- ПОРЯДОК привязок задан (`attached_at`, `instance_id`) — чтобы одинаковые события
-- давали одинаковую нагрузку и сравнение нагрузок не считало изменением
-- перестановку. Контракт при этом объявляет `attachments` НАБОРОМ, а не
-- упорядоченным полем: путь чтения порядка не задаёт вовсе, и обещать в потоке то,
-- чего не обещает `Get`, значило бы завести два контракта на одно поле.
CREATE FUNCTION kacho_storage.storage_outbox_volume_state(v_payload jsonb, v_id text)
    RETURNS jsonb LANGUAGE sql STABLE AS $$
    SELECT jsonb_build_object(
        'subscription.state',
        v_payload || jsonb_build_object('attachments', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                       'instance_id',   a.instance_id,
                       'instance_name', a.instance_name,
                       'device_name',   a.device_name,
                       'is_boot',       a.is_boot,
                       'mode',          a.mode,
                       'auto_delete',   a.auto_delete,
                       'attached_at',   a.attached_at)
                   ORDER BY a.attached_at, a.instance_id)
              FROM kacho_storage.volume_attachments a
             WHERE a.volume_id = v_id), '[]'::jsonb)));
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- storage_outbox_emit — та же эмиссия по строке РЕСУРСА; изменилось одно: у вида
-- «том» создание и правка кладут нагрузку В КОНВЕРТЕ.
--
-- Сравнение «что считается изменением» осталось на `storage_outbox_payload` —
-- нетронутой функции прежней миграции. Заворачивать сравниваемое было бы и дороже
-- (подзапрос на каждую правку дважды), и опаснее: конверт несёт привязки, а они у
-- OLD и NEW одной строки тома одинаковы, то есть в сравнении не участвуют и без
-- того. Один предмет — одно место, и оно прежнее.
CREATE OR REPLACE FUNCTION kacho_storage.storage_outbox_emit() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    v_kind text := TG_ARGV[0];
    v_was  jsonb;
    v_now  jsonb;
BEGIN
    IF TG_OP = 'DELETE' THEN
        -- Снятие — БЕЗ конверта: предмета больше нет, собирать нечего.
        INSERT INTO kacho_storage.storage_outbox
            (resource_kind, resource_id, project_id, event_type, payload)
        VALUES (v_kind, OLD.id, OLD.project_id, 'DELETED',
                kacho_storage.storage_outbox_payload(to_jsonb(OLD)));
        RETURN NULL;
    END IF;

    IF TG_OP = 'INSERT' THEN
        v_now := kacho_storage.storage_outbox_payload(to_jsonb(NEW));
        IF v_kind = 'Volume' THEN
            v_now := kacho_storage.storage_outbox_volume_state(v_now, NEW.id);
        END IF;
        INSERT INTO kacho_storage.storage_outbox
            (resource_kind, resource_id, project_id, event_type, payload)
        VALUES (v_kind, NEW.id, NEW.project_id, 'CREATED', v_now);
        RETURN NULL;
    END IF;

    v_was := kacho_storage.storage_outbox_payload(to_jsonb(OLD));
    v_now := kacho_storage.storage_outbox_payload(to_jsonb(NEW));
    IF v_was IS DISTINCT FROM v_now THEN
        IF v_kind = 'Volume' THEN
            v_now := kacho_storage.storage_outbox_volume_state(v_now, NEW.id);
        END IF;
        INSERT INTO kacho_storage.storage_outbox
            (resource_kind, resource_id, project_id, event_type, payload)
        VALUES (v_kind, NEW.id, NEW.project_id, 'UPDATED', v_now);
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- storage_outbox_emit_attachment — та же эмиссия по строке ПРИВЯЗКИ, объявляемая
-- правкой ТОМА; нагрузка теперь тоже в конверте.
--
-- Именно здесь конверт и окупается: привязка меняет ПУБЛИЧНЫЙ статус тома
-- (`AVAILABLE` ⇄ `IN_USE`), не трогая его строку. Событие снимается ПОСЛЕ
-- изменения, поэтому перечень привязок в конверте — состояние НА МОМЕНТ СОБЫТИЯ, а
-- не на момент чтения: снятая строка из снимка транзакции уже ушла, добавленная в
-- нём уже есть.
CREATE OR REPLACE FUNCTION kacho_storage.storage_outbox_emit_attachment() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    v_kind    text := TG_ARGV[0];
    v_volume  text;
    v_project text;
    v_now     jsonb;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_volume  := OLD.volume_id;
        v_project := OLD.project_id;
    ELSE
        v_volume  := NEW.volume_id;
        v_project := NEW.project_id;
    END IF;

    SELECT kacho_storage.storage_outbox_payload(to_jsonb(v)) INTO v_now
      FROM kacho_storage.volumes v
     WHERE v.id = v_volume;

    -- Тома нет: он снят в этой же транзакции, и его собственное снятие уже
    -- эмитировано. Второе событие о том же предмете было бы правкой снятого.
    IF v_now IS NULL THEN
        RETURN NULL;
    END IF;

    INSERT INTO kacho_storage.storage_outbox
        (resource_kind, resource_id, project_id, event_type, payload)
    VALUES (v_kind, v_volume, v_project, 'UPDATED',
            kacho_storage.storage_outbox_volume_state(v_now, v_volume));
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Возврат к журналу БЕЗ состояния предмета.
--
-- ЧТО ПРОИСХОДИТ С УЖЕ НАКОПЛЕННЫМ: строки, записанные в конверте, остаются как
-- есть и продолжают доезжать с состоянием — откат меняет то, что пишется ВПЕРЁД, а
-- не то, что уже записано. Переписывать их нечем и не нужно: сборщик прежней
-- редакции конверта не знает и ответит по ним «состояние не производится», то есть
-- тем же, чем по любой строке минимальной формы. Потери нет, есть возврат к
-- прежнему объёму обещания.
SET search_path TO kacho_storage, public;

CREATE OR REPLACE FUNCTION kacho_storage.storage_outbox_emit() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    v_kind text := TG_ARGV[0];
    v_was  jsonb;
    v_now  jsonb;
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO kacho_storage.storage_outbox
            (resource_kind, resource_id, project_id, event_type, payload)
        VALUES (v_kind, OLD.id, OLD.project_id, 'DELETED',
                kacho_storage.storage_outbox_payload(to_jsonb(OLD)));
        RETURN NULL;
    END IF;

    IF TG_OP = 'INSERT' THEN
        INSERT INTO kacho_storage.storage_outbox
            (resource_kind, resource_id, project_id, event_type, payload)
        VALUES (v_kind, NEW.id, NEW.project_id, 'CREATED',
                kacho_storage.storage_outbox_payload(to_jsonb(NEW)));
        RETURN NULL;
    END IF;

    v_was := kacho_storage.storage_outbox_payload(to_jsonb(OLD));
    v_now := kacho_storage.storage_outbox_payload(to_jsonb(NEW));
    IF v_was IS DISTINCT FROM v_now THEN
        INSERT INTO kacho_storage.storage_outbox
            (resource_kind, resource_id, project_id, event_type, payload)
        VALUES (v_kind, NEW.id, NEW.project_id, 'UPDATED', v_now);
    END IF;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION kacho_storage.storage_outbox_emit_attachment() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    v_kind    text := TG_ARGV[0];
    v_volume  text;
    v_project text;
    v_now     jsonb;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_volume  := OLD.volume_id;
        v_project := OLD.project_id;
    ELSE
        v_volume  := NEW.volume_id;
        v_project := NEW.project_id;
    END IF;

    SELECT kacho_storage.storage_outbox_payload(to_jsonb(v)) INTO v_now
      FROM kacho_storage.volumes v
     WHERE v.id = v_volume;

    IF v_now IS NULL THEN
        RETURN NULL;
    END IF;

    INSERT INTO kacho_storage.storage_outbox
        (resource_kind, resource_id, project_id, event_type, payload)
    VALUES (v_kind, v_volume, v_project, 'UPDATED', v_now);
    RETURN NULL;
END;
$$;

DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_volume_state(jsonb, text);
-- +goose StatementEnd
