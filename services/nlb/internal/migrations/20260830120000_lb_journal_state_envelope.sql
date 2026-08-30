-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up

-- =============================================================================
-- ВОСЬМАЯ ТОЧКА ЭМИССИИ ВИДА `nlb_load_balancer` НАЧИНАЕТ ПИСАТЬ СОСТОЯНИЕ
-- =============================================================================
-- Задача `PRO-Robotech/kacho#1551` — событие подписки несёт ПОЛНОЕ состояние
-- предмета у всех трёх видов журнала nlb. Точек эмиссии у этого вида девять, и
-- состояние обязаны нести восемь: семь на Go, восьмая — вот эта триггерная
-- функция пересчёта статуса (девятая эмитит СНЯТИЕ, у которого состояния не
-- бывает). До сих пор она писала минимальный снимок `{id, status, recomputed}`.
--
-- Обогащение обязано быть ВСЁ-ИЛИ-НИЧЕГО В ПРЕДЕЛАХ ВИДА: контракт единой формы
-- подписки разрешает читать НЕПУСТОЕ состояние как ПОЛНОЕ, поэтому одна точка,
-- отдающая частичный снимок, делает ложным весь вид — и делает это тихо.
--
-- -----------------------------------------------------------------------------
-- ФОРМА НАГРУЗКИ — СТРОКА ЦЕЛИКОМ, а не собранный по полям объект
-- -----------------------------------------------------------------------------
-- `to_jsonb(строки)` НЕ является второй проекцией ресурса: он ничего не выбирает
-- и ни о чём не судит. Вторую проекцию завёл бы `jsonb_build_object` по полям —
-- она стояла бы рядом с той, которой отвечает чтение, на другом языке, и
-- расходились бы они молча. Проекция в контракт остаётся ОДНА и живёт на Go
-- (`dto/type2pb`), общая с `Get`. Образец в дереве — журнал реестра
-- (`services/registry/internal/migrations/20260828154000_registry_resource_journal.sql`).
--
-- ЦЕНА, НАЗВАННАЯ ВСЛУХ: ключи получаются ИМЁН КОЛОНОК, а не имён полей Go.
-- Форма на проводе принята ОДНА — эта; сторона Go приведена к ней типом
-- `kachorepo.LoadBalancerJournalRow`, чьи теги суть имена этих колонок. Согласие
-- двух сторон держит не соглашение, а сквозная проба над НАСТОЯЩЕЙ схемой
-- (`TestLoadBalancerStateIsTheSameFromTheTriggerAndFromGo`): она заставляет этот
-- триггер сработать настоящим оператором и сверяет обе нагрузки на ОДНОЙ строке.
--
-- Вторая цена: `to_jsonb` подхватывает КАЖДУЮ колонку таблицы, включая
-- добавленную позже. Наружу это не выходит — публичную проекцию строит Go, и она
-- lean by construction: VIP-адрес, источник VIP и семейства в контракт не идут —
-- это правило двух проекций, по которому инфра-чувствительное живёт только на
-- внутренней поверхности. Но в самой таблице журнала они лягут, и
-- это надо помнить при всяком `ALTER TABLE load_balancers ADD COLUMN`: перестанет
-- журнал быть внутренним хранилищем сервиса — нагрузку придётся сузить ВЫЧИТАНИЕМ
-- названных колонок (`- 'address_v4' - …`), а не перечислением оставшихся:
-- перечисление и есть та самая вторая проекция.
--
-- ОТМЕТКИ ВРЕМЕНИ ПРИБИТЫ К UTC. `to_jsonb` рендерит `timestamptz` по часовому
-- поясу СЕССИИ — форма нагрузки зависела бы от настройки того, кто пишет.
--
-- -----------------------------------------------------------------------------
-- ПОЧЕМУ ТРИГГЕР ПРОДОЛЖАЕТ ЭМИТИТЬ, а не уступает место семи точкам Go
-- -----------------------------------------------------------------------------
-- Рассмотрен и отвергнут обратный исход: снять эмиссию отсюда и объявлять правку
-- из тех путей Go, которые этот триггер вызывают. Он требует НЕ ДВУХ новых точек,
-- а вечной бдительности: строку статуса меняет ЛЮБАЯ запись в `listeners` —
-- вставка, правка `status` либо `default_target_group_id`, снятие, — и сегодня
-- один из этих путей (правка привязки слушателя) не объявляет о балансировщике
-- ничего. Инвариант «статус пересчитан ⇒ событие есть» держится сейчас БАЗОЙ,
-- атомарно с самой записью статуса; вынести его значит заменить построение
-- соглашением, которое обязан помнить каждый следующий автор.
--
-- Цена ошибки у двух исходов РАЗНАЯ, и это решает. Здесь худшее — состояние
-- отсутствует, и подписчик получает честное `NOT_PRODUCED`. Там худшее — события
-- НЕТ ВОВСЕ: подписчик не узнаёт, что надо перечитать, и держит устаревшее
-- состояние сколько угодно долго, не имея ни одного наблюдаемого признака.
--
-- CREATE OR REPLACE (не правка 0001/0013/0022 — ban #5: применённые миграции
-- неизменяемы). Условие пересчёта и CAS-guard 0013 сохранены дословно: когда
-- строка журнала появляется — не меняется ни на один вход.
--
-- Меняются ДВЕ вещи, и вторая — не косметика:
--
--   1. НАГРУЗКА эмитируемой строки: минимальный снимок → конверт полного
--      состояния;
--   2. СВЯЗЬ эмиссии с применением пересчёта. Прежде она держалась ПРИЗНАКОМ
--      (`GET DIAGNOSTICS ... ROW_COUNT` плюс ветка), теперь — ПОСТРОЕНИЕМ: строка
--      журнала выбирается ИЗ ТОГО, ЧТО ВЕРНУЛ CAS, поэтому промах CAS не даёт
--      строки, потому что выбирать не из чего.
--
-- Второе сделано не ради краткости. Признак этот проверить нечем: промах CAS
-- требует конкурентного явного перехода, которого проба дёшево не создаёт, — а
-- цена ошибки в нём выросла вместе с формой нагрузки: событие с НЕЗАПОЛНЕННЫМ
-- состоянием подписчик читает по контракту формы как ПОЛНОЕ. Непроверяемый
-- признак заменён отсутствием признака.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_nlb.lb_status_recompute() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    affected_lb_id  text;
    cur_status      text;
    has_wired       boolean;
    new_status      text;
BEGIN
    -- The pivot is gone; only listener changes drive the recompute.
    IF TG_TABLE_NAME = 'listeners' THEN
        affected_lb_id := COALESCE(NEW.load_balancer_id, OLD.load_balancer_id);
    ELSE
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF affected_lb_id IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    -- Якорь проекта здесь БОЛЬШЕ НЕ ЧИТАЕТСЯ: строку журнала пишет вернувшая её
    -- строка CAS, и проект берётся оттуда же, откуда состояние. Прежняя редакция
    -- брала его снимком «до» — два источника у одного события, и расходиться им
    -- было где.
    SELECT status INTO cur_status
      FROM kacho_nlb.load_balancers
     WHERE id = affected_lb_id;

    -- LB deleted or absent — nothing to recompute.
    IF cur_status IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    -- Preserve explicit transitions (CREATING / DELETING).
    IF cur_status NOT IN ('INACTIVE','ACTIVE') THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    -- ACTIVE iff at least one non-DELETING listener is wired to a target group.
    SELECT EXISTS (
        SELECT 1 FROM kacho_nlb.listeners
         WHERE load_balancer_id = affected_lb_id
           AND status <> 'DELETING'
           AND default_target_group_id <> ''
    ) INTO has_wired;

    IF has_wired THEN
        new_status := 'ACTIVE';
    ELSE
        new_status := 'INACTIVE';
    END IF;

    IF new_status <> cur_status THEN
        -- CAS: write only if status was not moved out from under us by a
        -- concurrent explicit transition between our SELECT and this UPDATE.
        --
        -- ЭМИССИЯ СВЯЗАНА С ПРИМЕНЕНИЕМ ПЕРЕСЧЁТА ПО ПОСТРОЕНИЮ, а не признаком.
        -- Строка журнала берётся ИЗ ТОГО, ЧТО ВЕРНУЛ CAS: промах возвращает ноль
        -- строк, значит вставлять нечего и вставки не будет. Прежняя форма
        -- спрашивала об этом отдельно (`GET DIAGNOSTICS` плюс ветка), и признак
        -- этот проверить было нечем: промах CAS требует конкурентного явного
        -- перехода, которого проба дёшево не создаёт. Цена ошибки при том выросла
        -- бы: событие с НЕЗАПОЛНЕННЫМ состоянием подписчик читает по контракту
        -- формы как полное.
        --
        -- Состояние берётся ТЕМ ЖЕ оператором (`RETURNING`), а не читается следом:
        -- оно обязано быть состоянием НА МОМЕНТ СОБЫТИЯ. Второй запрос отвечал бы
        -- за момент чтения — в этой транзакции разница не наблюдаема, но
        -- становится наблюдаемой при первой же правке порядка, а построение
        -- `RETURNING` от порядка не зависит.
        --
        -- Якорь проекта тоже приходит из вернувшейся строки, а не из снимка «до»:
        -- у одного события один источник, и расходиться им негде.
        WITH applied AS (
            UPDATE kacho_nlb.load_balancers
               SET status = new_status
             WHERE id = affected_lb_id
               AND status = cur_status
            RETURNING *
        )
        INSERT INTO kacho_nlb.nlb_outbox
            (resource_type, resource_id, project_id, action, payload)
        SELECT
            'nlb_load_balancer',
            a.id,
            a.project_id,
            'UPDATED',
            jsonb_build_object(
                -- Конверт: ключ, которого прежняя, минимальная форма не писала
                -- ни разу. Полноту объявляет ОН, а не удача разбора — журнал не
                -- чистится, и строки прежней формы доезжают до сборщика
                -- состояния и сегодня.
                'state', to_jsonb(a) || jsonb_build_object(
                    'created_at', to_char(a.created_at AT TIME ZONE 'UTC',
                                          'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
                    'updated_at', to_char(a.updated_at AT TIME ZONE 'UTC',
                                          'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
                -- Диагностический маркер прежней формы сохранён СОСЕДОМ конверта,
                -- а не внутри него: он говорит, ЧЕМ вызвана правка, и к состоянию
                -- предмета не относится.
                'recomputed', true
            )
          FROM applied a;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Возврат тела к форме миграции 0022: минимальный снимок `{id, status, recomputed}`.
--
-- ЧТО ЭТО ЗНАЧИТ ДЛЯ ПОДПИСЧИКА, названо прямо: строки, записанные ДО отката,
-- конверт несут и разберутся; записанные ПОСЛЕ — не несут, и сборщик состояния
-- ответит по ним `NOT_PRODUCED`. Это честное отсутствие, а не ложь: отличать
-- строки без состояния от строк с ним умеет сам конверт, и откат не делает
-- ни одну прежнюю строку недостоверной.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_nlb.lb_status_recompute() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    affected_lb_id  text;
    cur_status      text;
    cur_project_id  text;
    has_wired       boolean;
    new_status      text;
    recomputed_rows integer;
BEGIN
    IF TG_TABLE_NAME = 'listeners' THEN
        affected_lb_id := COALESCE(NEW.load_balancer_id, OLD.load_balancer_id);
    ELSE
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF affected_lb_id IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    SELECT status, project_id INTO cur_status, cur_project_id
      FROM kacho_nlb.load_balancers
     WHERE id = affected_lb_id;

    IF cur_status IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF cur_status NOT IN ('INACTIVE','ACTIVE') THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    SELECT EXISTS (
        SELECT 1 FROM kacho_nlb.listeners
         WHERE load_balancer_id = affected_lb_id
           AND status <> 'DELETING'
           AND default_target_group_id <> ''
    ) INTO has_wired;

    IF has_wired THEN
        new_status := 'ACTIVE';
    ELSE
        new_status := 'INACTIVE';
    END IF;

    IF new_status <> cur_status THEN
        UPDATE kacho_nlb.load_balancers
           SET status = new_status
         WHERE id = affected_lb_id
           AND status = cur_status;
        GET DIAGNOSTICS recomputed_rows = ROW_COUNT;

        IF recomputed_rows > 0 THEN
            INSERT INTO kacho_nlb.nlb_outbox
                (resource_type, resource_id, project_id, action, payload)
            VALUES (
                'nlb_load_balancer',
                affected_lb_id,
                cur_project_id,
                'UPDATED',
                jsonb_build_object(
                    'id', affected_lb_id,
                    'status', new_status,
                    'recomputed', true
                )
            );
        END IF;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd
