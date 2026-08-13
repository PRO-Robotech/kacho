-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- Очередь аудита вычислений.
--
-- ЗАЧЕМ ОНА, ЕСЛИ ЕСТЬ ДВЕ ДРУГИЕ ОЧЕРЕДИ. Их предмет другой: одна несёт ленту
-- изменений для подписчика, вторая — намерения регистрации прав. Ни та, ни
-- другая не отвечает на вопрос «кто это сделал»: у события ленты нет актора, а
-- у намерения регистрации нет ни актора, ни глагола.
--
-- ПОЧЕМУ В ТОЙ ЖЕ ТРАНЗАКЦИИ, А НЕ ПОСЛЕ. Запись аудита, сделанная после
-- коммита мутации, теряется ровно тогда, когда она нужнее всего — процесс упал
-- между коммитом и записью. Обратный порядок хуже: аудит остаётся, мутация нет,
-- и журнал утверждает о действии, которого не было. Единственный порядок, при
-- котором журнал не врёт ни в одну сторону, — общая транзакция.
--
-- ДВА ЛИЦА, А НЕ ОДНО. Асинхронное продолжение исполняется под личностью
-- ИНИЦИАТОРА, захваченной в момент запроса: человек нажал «удалить», рабочая
-- функция доделывает под ним. Записать только исполнителя значит потерять
-- ответственность; записать только инициатора — потерять, кто фактически
-- выполнил. Пишутся оба.

-- +goose Up

CREATE TABLE audit_outbox (
    id                text PRIMARY KEY,

    -- Глагол в форме «ресурс.действие»: instance.create, instance.delete.
    -- Ограничение формы стоит на уровне БД, а не на уровне кода: свободная
    -- строка здесь означала бы, что журнал нельзя ни сгруппировать, ни
    -- отфильтровать без знания всех написаний, которые кто-то когда-то ввёл.
    event_type        text NOT NULL,

    -- Предмет действия. Тип нужен наравне с идентификатором: идентификатор
    -- глобально уникален by construction, но читатель журнала не обязан знать
    -- словарь префиксов, чтобы понять, о чём запись.
    resource_type     text NOT NULL,
    resource_id       text NOT NULL,

    -- Область: проект, в котором действие совершено. Пустая строка законна —
    -- есть действия уровня каталога, у которых проекта нет.
    project_id        text NOT NULL DEFAULT '',

    -- КТО ФАКТИЧЕСКИ ВЫПОЛНИЛ. Для синхронного пути совпадает с инициатором;
    -- для асинхронного продолжения — рабочая функция под личностью инициатора.
    actor_type        text NOT NULL,
    actor_id          text NOT NULL,

    -- ОТ ЧЬЕГО ИМЕНИ. Захватывается в момент запроса и переживает передачу в
    -- рабочую функцию. Совпадение с актором — норма, а не признак ошибки.
    on_behalf_of_type text NOT NULL DEFAULT '',
    on_behalf_of_id   text NOT NULL DEFAULT '',

    -- Что именно изменилось. Объект, а не массив и не скаляр: запись, которую
    -- нельзя расширить полем, придётся заменять целиком.
    payload           jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_at        timestamptz NOT NULL DEFAULT now(),

    -- Доставка наружу. Состояния закрыты перечнем: свободная строка сделала бы
    -- «застряло» и «опечатка в состоянии» неотличимыми.
    status            text NOT NULL DEFAULT 'pending',
    attempts          integer NOT NULL DEFAULT 0,
    next_attempt_at   timestamptz NOT NULL DEFAULT now(),
    sent_at           timestamptz,

    CONSTRAINT audit_outbox_id_check
        CHECK (id ~ '^aud[0-9A-HJKMNP-TV-Za-hjkmnp-tv-z]{17}$'),
    CONSTRAINT audit_outbox_event_type_check
        CHECK (event_type ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'
               AND length(event_type) BETWEEN 3 AND 128),
    CONSTRAINT audit_outbox_resource_type_check
        CHECK (length(resource_type) BETWEEN 1 AND 64),
    CONSTRAINT audit_outbox_resource_id_check
        CHECK (length(resource_id) BETWEEN 1 AND 128),
    CONSTRAINT audit_outbox_actor_check
        CHECK (length(actor_type) BETWEEN 1 AND 32 AND length(actor_id) BETWEEN 1 AND 128),
    -- Обе половины второго лица заполняются вместе либо не заполняются вовсе:
    -- «тип есть, идентификатора нет» — состояние, из которого нельзя понять,
    -- от чьего имени действовали.
    CONSTRAINT audit_outbox_on_behalf_pair_check
        CHECK ((on_behalf_of_type = '') = (on_behalf_of_id = '')),
    CONSTRAINT audit_outbox_payload_object_check
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT audit_outbox_status_check
        CHECK (status IN ('pending', 'in_flight', 'sent', 'failed')),
    CONSTRAINT audit_outbox_attempts_check
        CHECK (attempts >= 0),
    -- Отправленная запись обязана нести время отправки, неотправленная — не
    -- нести: иначе «доставлено» и «поле забыли заполнить» выглядят одинаково.
    CONSTRAINT audit_outbox_sent_at_check
        CHECK ((status = 'sent') = (sent_at IS NOT NULL))
);

-- Дренаж читает голову очереди по времени попытки; частичный индекс потому, что
-- доставленные записи в выборке дренажа не участвуют никогда, а таблица растёт
-- монотонно.
CREATE INDEX audit_outbox_pending_idx
    ON audit_outbox (next_attempt_at, id)
    WHERE status <> 'sent';

-- Чтение журнала по предмету: «что делали с этой машиной».
CREATE INDEX audit_outbox_resource_idx
    ON audit_outbox (resource_type, resource_id, created_at DESC);

-- Чтение журнала по области: «что делали в этом проекте».
CREATE INDEX audit_outbox_project_idx
    ON audit_outbox (project_id, created_at DESC)
    WHERE project_id <> '';

-- +goose Down

DROP TABLE IF EXISTS audit_outbox;
