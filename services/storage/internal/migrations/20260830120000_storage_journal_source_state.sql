-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin
-- Состояние ПРЕДМЕТА в журнале подписки — у видов «снимок» и «образ», вместе с
-- ПРОИЗВОДИТЕЛЕМ их событий.
--
-- Задача `PRO-Robotech/kacho#1556`. Предыдущие миграции журнала НЕ правятся
-- (ban #5): функции заменяются `CREATE OR REPLACE`, таблица и прежние триггеры
-- остаются те же.
--
--
-- ЧТО МЕШАЛО РАНЬШЕ — и почему довод «сделаем как у тома» доводом не был
--
-- Задача #1552 отдала состояние тому и НЕ отдала снимку с образом, назвав причину
-- точно: не «проекция выводится через таблицы» (входы нашлись бы и у них), а
-- ОТСУТСТВИЕ ПРОИЗВОДИТЕЛЯ СОБЫТИЙ. `used_by` снимка и образа — перечень ТОМОВ,
-- засеянных этим источником, а мутация ТОМА события источнику не эмитировала ни
-- один триггер. Отдай мы им состояние тогда — оно устаревало бы МОЛЧА: подписчик
-- держал бы снимок с пустым `used_by` при живых детях.
--
-- Производитель заводится здесь. Причина снята — не обойдена.
--
--
-- РАССМОТРЕН И ОТВЕРГНУТ ВТОРОЙ ПУТЬ: объявить `used_by` полем, которого поток НЕ несёт
--
-- Он дешевле, и задача честно называла его возможно более честным. Он неисполним, и
-- запрещает его не наше решение, а ПЛАТФОРМЕННЫЙ контракт подписки
-- (`proto/kacho/cloud/subscription/subscription.proto`, носитель нагрузки):
--
--     «Подписчик, получивший непустую нагрузку, вправе читать её как ПОЛНОЕ
--      состояние предмета — и это единственный случай, когда он вправе так делать.»
--
-- Выбор там — `oneof` из ДВУХ ветвей, и он про СОБЫТИЕ целиком: состояние либо
-- признак его отсутствия. Признака «поле не приехало» в этой форме нет вовсе —
-- значит состояние с вырезанным `used_by` неотличимо от состояния, у которого детей
-- нет. Подписчик, прямо уполномоченный контрактом читать непустую нагрузку как
-- полную, записал бы «детей нет» как ФАКТ и получил бы отказ удаления, которого его
-- собственное состояние не объясняет, — ровно ту ложь, ради устранения которой
-- состояние и вводится. Комментарий у поля этого не лечит: он адресован тому, кто
-- уже прочитал платформенное обещание и не имеет причин искать оговорку у владельца.
--
-- Третий путь (отдельный message состояния БЕЗ `used_by`) машинно различим — `Any`
-- несёт имя типа на проводе, — но заводит второй тип на вид, который надо вечно
-- держать в согласии с ресурсным, и заставляет storage отвечать на один вопрос
-- ДВУМЯ разными способами по своим же видам: у тома состояние есть ресурсный тип, у
-- снимка был бы свой. Продукт, противоречащий себе, — находка, а не решение.
--
--
-- ЦЕНА ИЗМЕРЕНА, И ОНА НИЖЕ, ЧЕМ ПРЕДПОЛАГАЛА ЗАДАЧА
--
-- Задача оценивала её как «мутация одного тома дала бы до двух лишних событий».
-- Перемерено по дереву: `used_by` источника — это `SELECT id FROM volumes WHERE
-- source_<вид>_id = <источник>`, поэтому перечень меняется РОВНО на трёх событиях
-- строки тома: появлении, снятии и смене самой ссылки. Ссылка же ИММУТАБЕЛЬНА —
-- `volume.proto` объявляет её таковой, а `Update` тома трогает только имя, описание,
-- метки и размер (`volumeUpdateSQL`), — и меняется она единственным способом:
-- `ON DELETE SET NULL` при удалении САМОГО источника, когда источника уже нет и
-- событие ему не полагается.
--
-- Значит цена не «на каждую мутацию тома», а РОВНО ОДНО лишнее событие на появление
-- сеянного тома и одно на его снятие. Правка меток, изменение размера, привязка,
-- отвязка и все подтверждения сверщика не эмитируют источнику НИЧЕГО.
--
-- Верхняя граница на один переход — два события (у тома по колонке на снимок и на
-- образ). Сегодня она недостижима: домен требует ровно одного источника. Триггер
-- судит колонки НЕЗАВИСИМО, поэтому останется верным, если это когда-нибудь
-- изменится, — но обещать «не более одного» на уровне схемы нечем, и делать вид,
-- что обещано, нельзя.
--
--
-- ПОЧЕМУ ЭТО НЕ ВТОРАЯ РЕАЛИЗАЦИЯ ПУБЛИЧНОЙ ПРОЕКЦИИ
--
-- Тот же довод, что у тома, и он здесь ещё сильнее. SQL не вычисляет ни одного
-- производного поля: он кладёт в конверт ровно те ВХОДЫ, что подаёт запрос чтения
-- своим коррелированным подзапросом (`snapshotSelectCols` / `imageSelectCols`), — и
-- ТЕМ ЖЕ порядком. Статус выводит единственная `domain.*StatusFromState`, проекцию
-- собирает единственный `protoconv.Snapshot` / `protoconv.Image`, обобщённую форму
-- «кем используется» — единственная `protoconv.seededBy`.
--
-- Ревизия привязки, которую запрос чтения подтягивает слева, в конверт НЕ едет и не
-- нужна: она кормит `backend_namespace` — поле ИНФРА-проекции, которого на публичной
-- поверхности нет вовсе.
--
--
-- ПОЧЕМУ У СНЯТИЯ КОНВЕРТА НЕТ — то же, что у тома
--
-- Предмета больше нет: собирать было нечего, попытки не было.
CREATE FUNCTION kacho_storage.storage_outbox_source_state(v_payload jsonb, v_kind text, v_id text)
    RETURNS jsonb LANGUAGE plpgsql STABLE AS $$
DECLARE
    v_seeded jsonb;
BEGIN
    -- Порядок — тот же, что у запроса чтения (`created_at`, `id`): одинаковое
    -- состояние обязано давать одинаковую нагрузку, иначе сравнение двух проекций
    -- считало бы изменением перестановку. Контракт при этом объявляет `used_by`
    -- НАБОРОМ; устойчивость здесь — свойство реализации, а не обещание.
    IF v_kind = 'Snapshot' THEN
        SELECT COALESCE(jsonb_agg(sv.id ORDER BY sv.created_at, sv.id), '[]'::jsonb)
          INTO v_seeded
          FROM kacho_storage.volumes sv WHERE sv.source_snapshot_id = v_id;
    ELSIF v_kind = 'Image' THEN
        SELECT COALESCE(jsonb_agg(sv.id ORDER BY sv.created_at, sv.id), '[]'::jsonb)
          INTO v_seeded
          FROM kacho_storage.volumes sv WHERE sv.source_image_id = v_id;
    ELSE
        -- Вид вне пары — ошибка ПРОГРАММИСТА, а не состояние данных. Молчаливый
        -- пустой перечень выдал бы источник без детей за источник без потомков.
        RAISE EXCEPTION 'storage_outbox_source_state: вид % не является источником томов', v_kind;
    END IF;

    -- Ключ конверта — тот же, что у тома, и по той же причине: журнал не чистится,
    -- строки ПРЕЖНЕЙ формы доезжают до сборщика и сегодня, и отличать их надо ПО
    -- ПОСТРОЕНИЮ. Точка в имени делает написание невозможным именем колонки, а
    -- ключи прежней нагрузки — именно имена колонок (`to_jsonb` строки).
    RETURN jsonb_build_object('subscription.state',
        v_payload || jsonb_build_object('seeded_volume_ids', v_seeded));
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- storage_outbox_emit — та же эмиссия по строке РЕСУРСА; изменилось одно: конверт
-- кладут теперь ВСЕ ТРИ вида, а не только том.
--
-- Сравнение «что считается изменением» осталось на `storage_outbox_payload` —
-- нетронутой функции первой миграции журнала. Заворачивать сравниваемое и дороже, и
-- опаснее: конверт источника несёт перечень детей, а он у OLD и NEW одной строки
-- СНИМКА одинаков, то есть в сравнении не участвует и без того.
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
        ELSE
            v_now := kacho_storage.storage_outbox_source_state(v_now, v_kind, NEW.id);
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
        ELSE
            v_now := kacho_storage.storage_outbox_source_state(v_now, v_kind, NEW.id);
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
-- storage_outbox_emit_source — ПРОИЗВОДИТЕЛЬ, которого не было: событие ИСТОЧНИКУ,
-- чей перечень засеянных томов изменился.
--
-- Якорь проекта берётся из строки САМОГО ИСТОЧНИКА, а не у тома. Сегодня они равны
-- (вставка тома требует источника того же проекта, и требует этого одним стейтментом
-- с вставкой), но событие принадлежит источнику, и брать его якорь у соседа значило
-- бы завести второй источник одного факта.
CREATE FUNCTION kacho_storage.storage_outbox_emit_source(v_kind text, v_id text) RETURNS void
    LANGUAGE plpgsql AS $$
DECLARE
    v_row     jsonb;
    v_project text;
BEGIN
    IF v_id IS NULL OR v_id = '' THEN
        RETURN;  -- том без источника этого вида — сообщать некому
    END IF;

    IF v_kind = 'Snapshot' THEN
        SELECT kacho_storage.storage_outbox_payload(to_jsonb(s)), s.project_id
          INTO v_row, v_project
          FROM kacho_storage.snapshots s WHERE s.id = v_id;
    ELSIF v_kind = 'Image' THEN
        SELECT kacho_storage.storage_outbox_payload(to_jsonb(i)), i.project_id
          INTO v_row, v_project
          FROM kacho_storage.images i WHERE i.id = v_id;
    ELSE
        RAISE EXCEPTION 'storage_outbox_emit_source: вид % не является источником томов', v_kind;
    END IF;

    -- Источника нет: он снят в этой же транзакции (и снял ссылку у тома через
    -- `ON DELETE SET NULL`), а его собственное снятие уже эмитировано. Второе
    -- событие о том же предмете было бы правкой снятого. Тот же приём, каким
    -- защищена эмиссия по привязке.
    IF v_row IS NULL THEN
        RETURN;
    END IF;

    INSERT INTO kacho_storage.storage_outbox
        (resource_kind, resource_id, project_id, event_type, payload)
    VALUES (v_kind, v_id, v_project, 'UPDATED',
            kacho_storage.storage_outbox_source_state(v_row, v_kind, v_id));
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- storage_outbox_emit_lineage — эмиссия по строке ТОМА, объявляемая правкой его
-- ИСТОЧНИКА.
--
-- Это второй в дереве случай, когда изменение одной строки объявляется правкой
-- ДРУГОГО предмета; первый — привязка, меняющая публичный статус тома. Здесь
-- строка тома меняет публичный `used_by` снимка либо образа, не трогая их строк.
--
-- Событие снимается ПОСЛЕ изменения, поэтому перечень в конверте — состояние НА
-- МОМЕНТ СОБЫТИЯ: снятая строка из снимка транзакции уже ушла, добавленная в нём
-- уже есть.
--
-- НА ПРАВКЕ судятся колонки, а не факт правки: перечень источника меняет ТОЛЬКО
-- смена самой ссылки. Эмитировать на каждую правку тома значило бы будить
-- подписчиков снимка на переименовании чужого тома.
CREATE FUNCTION kacho_storage.storage_outbox_emit_lineage() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        PERFORM kacho_storage.storage_outbox_emit_source('Snapshot', NEW.source_snapshot_id);
        PERFORM kacho_storage.storage_outbox_emit_source('Image',    NEW.source_image_id);
        RETURN NULL;
    END IF;

    IF TG_OP = 'DELETE' THEN
        PERFORM kacho_storage.storage_outbox_emit_source('Snapshot', OLD.source_snapshot_id);
        PERFORM kacho_storage.storage_outbox_emit_source('Image',    OLD.source_image_id);
        RETURN NULL;
    END IF;

    -- Колонки судятся НЕЗАВИСИМО: домен требует ровно одного источника, но схема
    -- этого не обещает, и проверка «одна из двух» разошлась бы с ней молча.
    IF OLD.source_snapshot_id IS DISTINCT FROM NEW.source_snapshot_id THEN
        PERFORM kacho_storage.storage_outbox_emit_source('Snapshot', OLD.source_snapshot_id);
        PERFORM kacho_storage.storage_outbox_emit_source('Snapshot', NEW.source_snapshot_id);
    END IF;
    IF OLD.source_image_id IS DISTINCT FROM NEW.source_image_id THEN
        PERFORM kacho_storage.storage_outbox_emit_source('Image', OLD.source_image_id);
        PERFORM kacho_storage.storage_outbox_emit_source('Image', NEW.source_image_id);
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER volumes_storage_outbox_emit_lineage
    AFTER INSERT OR UPDATE OR DELETE ON kacho_storage.volumes
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.storage_outbox_emit_lineage();

-- +goose Down
-- +goose StatementBegin
-- Возврат к журналу, где состояние несёт ОДИН вид — том.
--
-- ЧТО ПРОИСХОДИТ С УЖЕ НАКОПЛЕННЫМ: строки, записанные в конверте, остаются как
-- есть и продолжают доезжать с состоянием — откат меняет то, что пишется ВПЕРЁД, а
-- не то, что уже записано. Сборщик прежней редакции конверта у снимка и образа не
-- знает и ответит по ним «не производится», то есть тем же, чем по любой строке
-- минимальной формы. Потери нет, есть возврат к прежнему объёму обещания.
SET search_path TO kacho_storage, public;

DROP TRIGGER IF EXISTS volumes_storage_outbox_emit_lineage ON kacho_storage.volumes;
DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_emit_lineage();
DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_emit_source(text, text);

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

DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_source_state(jsonb, text, text);
-- +goose StatementEnd
