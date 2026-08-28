-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin
-- Ресурсный журнал storage — возврат предмета, снятого 0011, вместе с его
-- потребителем.
--
-- Задача `PRO-Robotech/kacho#1414` (провязка общего сервера потока,
-- `pkg/subscription`).
--
-- ЗАЧЕМ. 0011 сняла `storage_outbox` как очередь БЕЗ потребителя и назвала
-- условие возврата дословно: «Он вводится обратно вместе с этим сервисом». Здесь
-- вводится и он, и сервис: общий сервер потока изменений на внутреннем
-- слушателе. Потребитель измерен, а не предположен — консоль опрашивает четыре
-- списка storage каждые 3 секунды и карточку каждые 5
-- (`ui-future/shared/src/lib/use-resource-list.ts`, `refetchInterval: 3_000`;
-- `ui-future/storage/src/components/organisms/ResourceShell/ResourceShell.tsx`,
-- `refetchInterval: 5_000`).
--
-- ФОРМА ОТЛИЧАЕТСЯ ОТ СНЯТОЙ, и это надо сказать прямо, потому что имя то же.
-- Ушли `processed_at` и индекс по виду: у журнала нет дренажа, его читают
-- курсором по номеру. Пришёл `project_id` — АВТОРИЗУЕМЫЙ ЯКОРЬ, которого у
-- прежней формы не было вовсе.
--
--
-- ПОЧЕМУ ПИШЕТ ТРИГГЕР, А НЕ ВЫЗОВ В КАЖДОМ МЕСТЕ ЗАПИСИ
--
-- У соседних владельцев журнала (compute, nlb) строку пишет репозиторий. Для
-- storage это негодно, и негодно по замеру, а не по вкусу: писателей ресурсных
-- строк здесь БОЛЬШЕ, чем репозиториев.
--
--   * СВЕРЩИК (`internal/reconciler/store.go`) мутирует `volumes`, `snapshots` и
--     `images` МИМО репозиториев, прямо на пуле. Его `Confirm` переводит строку
--     `CREATING → READY` — то есть совершает ровно тот переход, ради которого
--     консоль и опрашивает списки. Журнал, эмитирующий только из репозиториев,
--     об этом переходе не узнал бы, и подписка не заменила бы опрос.
--   * `Forget` того же сверщика снимает строку `DELETE … WHERE id = $1 AND
--     state = 'DELETING'` — БЕЗ `RETURNING project_id`. При вызове-эмиссии якорь
--     снятию взять неоткуда, а снятие без якоря не проходит ось проекта: клиент,
--     снявший опрос, держал бы удалённый том вечно.
--   * Без транзакции идут и `VolumeRepo.Detach`, `VolumeRepo.ChangeDiskType`,
--     `SnapshotRepo.Copy`, `ImageRepo.Copy`.
--
-- Триггер закрывает всё это ПО ПОСТРОЕНИЮ: он исполняется в той же транзакции,
-- что и мутация, всегда; якорь берёт из самой строки (`NEW.project_id` /
-- `OLD.project_id`), поэтому у снятия он есть; и шестой писатель, который
-- появится завтра, попадёт в журнал, не зная о журнале. Прецедент в дереве есть:
-- журнал vpc пишет в том числе триггер базы, а на этих же трёх таблицах уже
-- висят триггеры учёта пределов (0023).
--
--
-- ЧТО ИМЕННО СЧИТАЕТСЯ ИЗМЕНЕНИЕМ — и почему это НЕ перечень колонок
--
-- Сверщик выполняет `Observe` на КАЖДОМ проходе: `UPDATE … SET observed_state =
-- $2, observed_at = now()`. Без сужения журнал получал бы событие на каждый тик
-- по каждому ресурсу — то есть подписка была бы шумнее опроса, который она
-- заменяет.
--
-- Сужение сделано СРАВНЕНИЕМ НАГРУЗОК, а не списком колонок в условии триггера:
-- `storage_outbox_payload` — ОДНА функция, которая решает и что уезжает в
-- нагрузке, и что считается изменением. Два списка об одном предмете разошлись бы
-- молча, и разошлись бы там, где расхождение невидимо: колонка, выпавшая из
-- одного списка, перестаёт будить подписчика, оставаясь в нагрузке.
--
-- Исключаются ИНФРА-колонки, и по каждой назван довод:
--
--   binding_id, desired_binding_id — ревизия привязки к бэкенду. На публичной
--       поверхности `storagev1.Volume` их нет; расхождение этих двух выводило бы
--       статус переезда, но производителя у `domain.DeriveMigrating` в дереве
--       сегодня НОЛЬ, то есть публичного факта за ними не стоит.
--   backend_object, backend_namespace — адрес объекта у бэкенда: имя и
--       пространство имён. Это `wiring` из §«Инфра-чувствительные данные»
--       (`security.md`) — на тенантскую поверхность не выходит.
--   observed_state, observed_at, observed_size_bytes — НАБЛЮДЕНИЕ, а не факт о
--       ресурсе для арендатора: это то, что сверщик увидел у бэкенда, и меняется
--       оно каждым проходом.
--
-- `used_bytes` НЕ исключён намеренно, хотя пишет его тот же `Confirm`:
-- потребление ТЕНАНТСКОЕ (`storagev1.Volume.used_bytes`, необязательное поле —
-- «бэкенд не сказал» отличается от «пусто»). Исключив его, мы перестали бы будить
-- подписчика на изменение, которое он видит.
--
-- Отбор решает и второй вопрос — БЕЗОПАСНОСТЬ. Журнал кормит ручку, доступную
-- арендатору; инфра-колонки, попав в нагрузку, ждали бы там первого, кто решит
-- нагрузку отдавать.
--
--
-- ПОЧЕМУ ТРИГГЕР ЕСТЬ И НА `volume_attachments`
--
-- Привязка тома к машине НЕ ТРОГАЕТ строку `volumes` — `VolumeRepo.Attach`
-- вставляет строку в `volume_attachments`, и только. При этом публичный статус
-- тома меняется: `domain.DeriveStatus(state, attached)` даёт `IN_USE` вместо
-- `AVAILABLE` по существованию этой строки. Без триггера на привязке подписчик,
-- снявший опрос, НЕ УЗНАЛ БЫ о привязке и отвязке ни разу — а это самое частое
-- изменение тома за его жизнь.
--
-- Событие при этом объявляется изменением ТОМА (`resource_kind = 'Volume'`,
-- предмет — `volume_id`), а не собственным видом: привязка не адресуется
-- арендатором, у неё нет ни своего типа в модели прав, ни своей страницы.
--
--
-- ЧЕГО ЗДЕСЬ НЕТ И ПОЧЕМУ
--
-- Типы дисков, бэкенды хранения и привязки типов триггера не получают. У их
-- таблиц НЕТ колонки `project_id` (`0003_storage_domain.sql` для `disk_types`,
-- `0015_storage_backends_and_bindings.sql` для двух других), то есть якорь брать
-- неоткуда, и типа объекта модели прав у них тоже нет. Строка без якоря и без
-- типа не может быть авторизована: вопрос «вправе ли вызывающий её видеть»
-- задать НЕЧЕМ, и сервер такую строку не доставил бы, оставаясь зелёным. Это
-- исключение по ПРЕДМЕТУ, а не по недосмотру.
--
-- `CHECK` заведён только на род изменения, не на вид предмета. Род пишется
-- литералами внутри триггерной функции, и неизвестное слово тихо делает строку
-- недоставляемой — у него обязан быть второй, независимый производитель. Вид же
-- приходит АРГУМЕНТОМ навешивания и целиком виден в тексте этой миграции, где его
-- и сверяет разбор (`subscriptionjournal/producer_test.go`). Третье место об
-- одном предмете без своего гейта разошлось бы молча.

SET search_path TO kacho_storage, public;

CREATE TABLE kacho_storage.storage_outbox (
    -- Номер выдаётся счётчиком на ВСТАВКЕ, а строка становится видимой на
    -- ФИКСАЦИИ, поэтому порядок номеров и порядок фиксаций независимы. Границу
    -- устоявшегося держит общий сервер (`pkg/subscription/watermark.go`), а не
    -- эта таблица; здесь номер — только возрастающая координата.
    sequence_no   BIGSERIAL   PRIMARY KEY,
    resource_kind TEXT        NOT NULL,
    resource_id   TEXT        NOT NULL,
    -- Якорь проекта. Умолчание пустой строкой, а не NULL, — та же форма, что у
    -- соседних журналов; пусто по контракту означает «предмет уровня аккаунта
    -- или кластера», и для storage такого предмета нет ни одного.
    project_id    TEXT        NOT NULL DEFAULT '',
    event_type    TEXT        NOT NULL,
    payload       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT storage_outbox_event_type_known
        CHECK (event_type IN ('CREATED', 'UPDATED', 'DELETED'))
);

-- Частичный индекс, а не полный: строки без якоря по нему не отбираются никогда
-- (подписка с осью проекта их не спрашивает), и держать их в индексе значило бы
-- платить за то, что не читается. Пара «якорь, номер» — ровно порядок, которым
-- читает поток.
--
-- БЕЗ параллельного режима, и это решение, а не копия соседнего. У compute индекс
-- собирается `CONCURRENTLY` под аннотацией отказа от транзакции, потому что там
-- он ложится на ЖИВУЮ таблицу с идущей записью, и обычная сборка заставила бы
-- ждать каждую вставку в журнал — то есть каждую мутацию ресурса. Здесь таблица
-- СОЗДАЁТСЯ этой же миграцией и до её конца писателей не имеет: блокировать
-- нечего. Взять параллельный режим значило бы разбить миграцию на неатомарные
-- шаги без единого выигрыша — и тогда обрыв оставил бы таблицу с триггерами, но
-- без индекса, а сборка умеет оставить и негодный индекс.
--
-- Имя аннотации здесь НЕ воспроизводится дословно намеренно: разборщик миграций
-- ищет её в любой строке комментария и отвергает файл, встретив её в прозе. Это
-- измерено — первая редакция этого абзаца роняла накат целиком.
CREATE INDEX storage_outbox_project_idx
    ON kacho_storage.storage_outbox (project_id, sequence_no) WHERE project_id <> '';
-- +goose StatementEnd

-- +goose StatementBegin
-- storage_outbox_payload — ЕДИНСТВЕННОЕ место, решающее две вещи сразу: что
-- уезжает в нагрузке и что считается изменением. Довод по каждой снятой колонке —
-- в заголовке миграции.
CREATE FUNCTION kacho_storage.storage_outbox_payload(row_json jsonb) RETURNS jsonb
    LANGUAGE sql IMMUTABLE AS $$
    SELECT row_json
         - 'binding_id'
         - 'desired_binding_id'
         - 'backend_object'
         - 'backend_namespace'
         - 'observed_state'
         - 'observed_at'
         - 'observed_size_bytes';
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- storage_outbox_notify будит подписчиков. Канал назван БЕЗ схемы: имя канала —
-- не идентификатор объекта, и общая форма держит его отдельным полем именно
-- потому, что вывести его из имени схемо-квалифицированной таблицы нельзя.
CREATE FUNCTION kacho_storage.storage_outbox_notify() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('storage_outbox', NEW.sequence_no::text);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER storage_outbox_notify_trg
    AFTER INSERT ON kacho_storage.storage_outbox
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.storage_outbox_notify();

-- +goose StatementBegin
-- storage_outbox_emit — эмиссия по строке РЕСУРСА. Вид предмета приходит
-- аргументом навешивания: имя таблицы для этого не годится (см. эмиссию по
-- привязке ниже, где вид другой, чем таблица).
CREATE FUNCTION kacho_storage.storage_outbox_emit() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    v_kind text := TG_ARGV[0];
    v_was  jsonb;
    v_now  jsonb;
BEGIN
    IF TG_OP = 'DELETE' THEN
        -- Якорь берётся из СНИМАЕМОЙ строки — то есть существует. Ради этого
        -- триггер и выбран: путь снятия у сверщика не возвращает проекта вовсе.
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

    -- Правка. Событие эмитируется, ТОЛЬКО если строка изменилась по существу:
    -- наблюдение сверщика существом не является и в нагрузку не входит, поэтому
    -- сравнение нагрузок и есть сужение. Отдельного перечня колонок здесь нет
    -- намеренно — он разошёлся бы с перечнем функции нагрузки молча.
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
-- +goose StatementEnd

-- +goose StatementBegin
-- storage_outbox_emit_attachment — эмиссия по строке ПРИВЯЗКИ, объявляемая
-- изменением ТОМА.
--
-- Привязка не адресуется арендатором и своего типа в модели прав не имеет, зато
-- меняет публичный статус тома (`AVAILABLE` ⇄ `IN_USE`), не трогая его строку.
-- Без этой эмиссии подписчик не узнавал бы о самом частом изменении тома за его
-- жизнь.
CREATE FUNCTION kacho_storage.storage_outbox_emit_attachment() RETURNS trigger
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

    -- Нагрузка — строка ТОМА, а не привязки: предмет события том.
    SELECT kacho_storage.storage_outbox_payload(to_jsonb(v)) INTO v_now
      FROM kacho_storage.volumes v
     WHERE v.id = v_volume;

    -- Тома нет: он снят в этой же транзакции, и его собственное снятие уже
    -- эмитировано. Второе событие о том же предмете было бы правкой снятого.
    -- Сегодня эта ветка недостижима (ссылка привязки объявлена `ON DELETE
    -- RESTRICT`, том с привязками не удаляется), и оставлена она как
    -- разграничение, а не как защита от известного случая.
    IF v_now IS NULL THEN
        RETURN NULL;
    END IF;

    INSERT INTO kacho_storage.storage_outbox
        (resource_kind, resource_id, project_id, event_type, payload)
    VALUES (v_kind, v_volume, v_project, 'UPDATED', v_now);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER volumes_storage_outbox_emit
    AFTER INSERT OR UPDATE OR DELETE ON kacho_storage.volumes
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.storage_outbox_emit('Volume');

CREATE TRIGGER snapshots_storage_outbox_emit
    AFTER INSERT OR UPDATE OR DELETE ON kacho_storage.snapshots
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.storage_outbox_emit('Snapshot');

CREATE TRIGGER images_storage_outbox_emit
    AFTER INSERT OR UPDATE OR DELETE ON kacho_storage.images
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.storage_outbox_emit('Image');

CREATE TRIGGER volume_attachments_storage_outbox_emit
    AFTER INSERT OR UPDATE OR DELETE ON kacho_storage.volume_attachments
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.storage_outbox_emit_attachment('Volume');

-- +goose Down
-- +goose StatementBegin
-- Возврат в состояние без журнала.
--
-- ЧТО ТЕРЯЕТСЯ И НЕ ВОССТАНОВИТСЯ: накопленные строки. Восстанавливать их не из
-- чего — журнал производится мутациями, а не выводится из состояния. Подписчик,
-- державший позицию, при повторном применении начнёт с пустого журнала: его
-- сохранённая позиция окажется за пределами удержанного, и общий сервер ответит
-- ему тем, чем и должен, — «позиция утрачена».
--
-- Сказано прямо, потому что читать это будут при откате и под давлением. Потеря
-- неизбежна и сама по себе не порок — порок был бы в обещании восстановления,
-- которого не происходит.
SET search_path TO kacho_storage, public;

DROP TRIGGER IF EXISTS volume_attachments_storage_outbox_emit ON kacho_storage.volume_attachments;
DROP TRIGGER IF EXISTS images_storage_outbox_emit ON kacho_storage.images;
DROP TRIGGER IF EXISTS snapshots_storage_outbox_emit ON kacho_storage.snapshots;
DROP TRIGGER IF EXISTS volumes_storage_outbox_emit ON kacho_storage.volumes;
DROP TRIGGER IF EXISTS storage_outbox_notify_trg ON kacho_storage.storage_outbox;

DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_emit_attachment();
DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_emit();
DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_notify();

DROP TABLE IF EXISTS kacho_storage.storage_outbox;

DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_payload(jsonb);
-- +goose StatementEnd
