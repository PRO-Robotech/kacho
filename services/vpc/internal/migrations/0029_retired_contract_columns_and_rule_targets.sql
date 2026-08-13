-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Снятое с контракта пережило свои строки — три предмета, одна миграция.
-- =============================================================================
-- Снять поле с контракта и убрать за ним данные — разные действия, и второе тут
-- не выполнялось трижды. Пока строка стоит, она либо ничего не значит (тогда её
-- читатель однажды решит, что значит), либо значит то, чего продукт больше не
-- умеет (тогда она мешает).
--
--  1. `subnets.dhcp_options` — колонка baseline. Контракт снят целиком: номер и
--     имя зарезервированы в `Subnet`/`CreateSubnetRequest`/`UpdateSubnetRequest`,
--     ни один RPC поля не принимает и не отдаёт. Значение никто не задавал и
--     никто не читал; репозиторий его лишь возил туда-обратно. Снимается вместе
--     с Go-типом и его сериализацией — иначе осталась бы колонка, у которой есть
--     писатель и нет автора значения.
--
--  2. `networks.route_distinguisher` — колонка baseline. Читателей в прод-коде
--     ноль. Предикат назван так, чтобы он остался ВЕРНЫМ и после этой миграции
--     (иначе он лжёт с первого же дня): `git grep -In route_distinguisher --
--     ':!*docs*' ':!*/tests/*' ':!*/migrations/*'` — пусто. Каталог миграций
--     исключён намеренно: имя колонки обязано в нём стоять — в 0001, которая её
--     создала, и здесь, где она снимается. Публичная проекция сети её не несла.
--     ЭТО НЕ `vrf_id`. `vrf_id` — живой internal-only инфра-идентификатор,
--     аллоцируемый 0007, читаемый `InternalNetworkService` по правилу двух
--     проекций; он остаётся, и проба это утверждает отдельным условием.
--
--  3. `security_groups.rules` — правила, сериализованные со снятой ветвью цели.
--     Ветвь предопределённой цели снята с контракта; домен её СЕРИАЛИЗОВАЛ, и в
--     JSONB могли остаться объекты с ключом `PredefinedTarget`. Go-структура
--     этого поля больше не имеет, поэтому при чтении ключ молча отбрасывается —
--     и правило проецируется БЕЗ цели. По закрытой модели правило без цели не
--     разрешает НИЧЕГО (см. `validateSGRuleTarget`): арендатор получал успех на
--     правиле, которое не делает написанного.
--     Хуже — оно блокирует правку: цель теперь обязательна, поэтому «прочитать
--     группу и записать обратно» отвечает отказом на правиле, которого
--     вызывающий не писал, и группа перестаёт быть редактируемой через API.
--
-- Почему у пункта 3 не одна судьба на все правила, а разбор по видам
-- -----------------------------------------------------------------------------
-- Значений у снятой ветви было два, и они РАЗНЫЕ по выразимости:
--
--   * `self_security_group` СОХРАНЯЕТ СМЫСЛ: «цель — сама эта группа», и это
--     ровно то, что выражает живая ветвь `security_group_id`, указывающая на id
--     самой группы. Правило нормализуется в неё — намерение сохранено целиком, а
--     не приблизительно. Same-network-проверка на такой цели тождественно
--     выполняется: группа и цель — одна строка, значит одна сеть.
--
--   * `loadbalancer_healthchecks` выразимого соответствия НЕ имеет. Исходов у
--     него было два, оба плохие, и выбран менее плохой:
--       — оставить: правило остаётся в наборе, не разрешает ничего (цели нет) и
--         при этом делает группу нередактируемой. То есть намерение НЕ
--         исполняется И починить группу через API нельзя;
--       — снять: правило исчезает из проекции. Правила группы — РАЗРЕШАЮЩИЕ,
--         поэтому снятие разрешающего правила доступ не расширяет; а конкретно
--         это правило уже не разрешало ничего, значит фактический доступ не
--         меняется вовсе — меняется только то, что арендатор видит в `Get`.
--     Выбрано снятие. Придумывать замену запрещено: подстановка любого блока
--     адресов или «любой цели» РАСШИРИЛА бы правило, то есть выдала бы доступ,
--     которого арендатор не просил, — это хуже обоих исходов.
--     Цена названа прямо: снятие меняет то, что арендатор написал. Поэтому
--     миграция не молчит — каждая группа, потерявшая правило, называется по id
--     в NOTICE (ниже), чтобы владельцу стенда было ЧТО сказать арендатору.
--
-- Ещё два вида, которые разбор обязан различать, иначе он ломает исправное:
--
--   * правило со снятым ключом И живой целью — ключ снимается, цель остаётся как
--     есть. Переписывать её на самоссылку значило бы менять действующее правило
--     из-за мёртвого ключа рядом;
--   * правило БЕЗ цели вовсе (и без снятого ключа) — тот же самый дефект и тот
--     же исход, снятие. Это и есть класс, а не экземпляр: инертное правило,
--     блокирующее правку группы, приходит и без участия снятой ветви, потому что
--     цель не проверялась вообще. Починить только вид с ключом значило бы закрыть
--     находку и оставить класс.
--
-- Чего эта миграция НЕ решает, и это осознанно
-- -----------------------------------------------------------------------------
--   * Правило с ДВУМЯ живыми целями (блоки адресов и группа одновременно)
--     контрактом тоже не выразимо — на проводе `oneof` держит одну. Но выбрать,
--     какую оставить, значит придумать намерение, поэтому такие правила НЕ
--     трогаются: они считаются и называются в NOTICE. Ни один путь продукта их
--     не производил (в форме передачи это `oneof`), поэтому ожидаемое число — 0;
--     если оно окажется другим, это отдельное решение с отдельным предметом.
--   * Ограничение БД «ровно одна цель» здесь НЕ вводится. Оно потребовало бы
--     решить судьбу двухцелевых строк (см. выше) в той же миграции, то есть
--     свести два разных вопроса о намерении арендатора в одно необратимое
--     действие. Инвариант остаётся синхронной проверкой use-case до тех пор.
--
-- Наблюдаемость: почему числа печатаются, а не заявляются
-- -----------------------------------------------------------------------------
-- Правка внутри JSONB и снятие КОЛОНКИ манифестом `dropguard.json` не
-- выражаются — он считает ТАБЛИЦЫ (`internal/dropguard`, `DROP TABLE` в Up), и
-- строка-колонка в него не помещается by construction. Поэтому счётчики здесь —
-- печать, ровно как в 0027, и печатается ОБЪЁМ ОСМОТРЕННОГО рядом с объёмом
-- изменённого: «ноль изменённых» обязано быть отличимо от «ноль осмотренных».
-- Печать проверяется пробой, а не обещанием: `migration_0029_retired_contract_
-- integration_test.go` в ЭТОМ каталоге ловит NOTICE соединения (обработчик
-- `OnNotice` на конфигурации pgx) и утверждает числа по каждому виду — в том числе
-- на наборе, где менять нечего, потому что именно там печать и обязана говорить.
-- Основание, на которое опирается необратимость: директива владельца 2026-07-27
-- «облако не в проде, тенантских данных нет». Перестанет быть верным — числа
-- обязаны стать утверждением, а не остаться NOTICE.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- Нормализация правил со снятой ветвью цели.
--
-- Разбор по видам идёт ОДНИМ проходом на группу, а решение по каждому правилу —
-- выражением, а не последовательностью UPDATE'ов: иначе промежуточное состояние
-- («ключ снят, цель ещё не выставлена») было бы наблюдаемо, а на нём правило
-- выглядит бесцельным и попало бы под снятие следующим же шагом.
--
-- Предикаты типов вычисляются через CASE, а не через AND: `jsonb_array_length`
-- на не-массиве ВОЗБУЖДАЕТ исключение, а порядок вычисления операндов AND в SQL
-- не гарантирован. CASE порядок гарантирует. Та же причина, что в 0027.
DO $$
DECLARE
    groups_examined bigint := 0;
    rules_examined  bigint := 0;
    rows_rewritten  bigint := 0;
    self_targeted   bigint := 0;
    keys_stripped   bigint := 0;
    rules_dropped   bigint := 0;
    ambiguous_left  bigint := 0;
    g               record;
    new_rules       jsonb;
    n_self          bigint;
    n_strip         bigint;
    n_drop          bigint;
    n_amb           bigint;
BEGIN
    -- Осмотренное — ВСЕ группы, включая те, чей набор пуст или записан
    -- JSON-скаляром `null` (маршалинг Go nil-slice). Считать только «подходящие»
    -- значило бы напечатать объём находок под именем объёма чтения.
    SELECT count(*),
           COALESCE(sum(CASE WHEN jsonb_typeof(rules) = 'array'
                             THEN jsonb_array_length(rules) ELSE 0 END), 0)
      INTO groups_examined, rules_examined
      FROM kacho_vpc.security_groups;

    FOR g IN
        SELECT id, rules
          FROM kacho_vpc.security_groups
         WHERE CASE WHEN jsonb_typeof(rules) = 'array'
                    THEN jsonb_array_length(rules) > 0 ELSE false END
         ORDER BY id
    LOOP
        WITH elems AS (
            SELECT t.ord, t.value AS rule
              FROM jsonb_array_elements(g.rules) WITH ORDINALITY AS t(value, ord)
        ),
        classified AS (
            SELECT
                ord,
                rule,
                -- Ветвь `cidr_blocks` несёт ОБА семейства, поэтому «v4 и v6
                -- вместе» — ОДНА цель. Считать их по отдельности значило бы
                -- объявить законное двухсемейное правило двухцелевым.
                (CASE WHEN (CASE WHEN jsonb_typeof(rule -> 'V4CidrBlocks') = 'array'
                                 THEN jsonb_array_length(rule -> 'V4CidrBlocks') > 0
                                 ELSE false END)
                        OR (CASE WHEN jsonb_typeof(rule -> 'V6CidrBlocks') = 'array'
                                 THEN jsonb_array_length(rule -> 'V6CidrBlocks') > 0
                                 ELSE false END)
                      THEN 1 ELSE 0 END)
              + (CASE WHEN jsonb_typeof(rule -> 'SecurityGroupID') = 'string'
                           AND (rule ->> 'SecurityGroupID') <> ''
                      THEN 1 ELSE 0 END) AS live_kinds,
                CASE WHEN jsonb_typeof(rule -> 'PredefinedTarget') = 'string'
                     THEN rule ->> 'PredefinedTarget' ELSE '' END AS retired_target,
                (rule ? 'PredefinedTarget') AS has_retired_key
              FROM elems
        ),
        decided AS (
            SELECT
                ord,
                live_kinds,
                CASE
                    -- Живая цель решает сама за себя; мёртвый ключ рядом лишь
                    -- снимается.
                    WHEN live_kinds > 0 AND has_retired_key THEN 'strip'
                    WHEN live_kinds > 0                     THEN 'intact'
                    -- Единственное значение снятой ветви, у которого есть точное
                    -- живое соответствие.
                    WHEN retired_target = 'self_security_group' THEN 'self'
                    -- Всё остальное: снятая ветвь без соответствия ИЛИ правило
                    -- вообще без цели. Оба не разрешают ничего и оба блокируют
                    -- правку группы.
                    ELSE 'drop'
                END AS cls,
                CASE
                    WHEN live_kinds > 0 AND has_retired_key
                        THEN rule - 'PredefinedTarget'
                    WHEN live_kinds > 0
                        THEN rule
                    WHEN retired_target = 'self_security_group'
                        THEN jsonb_set(rule - 'PredefinedTarget',
                                       '{SecurityGroupID}', to_jsonb(g.id))
                    ELSE NULL
                END AS kept
              FROM classified
        )
        SELECT COALESCE(jsonb_agg(kept ORDER BY ord) FILTER (WHERE cls <> 'drop'),
                        '[]'::jsonb),
               count(*) FILTER (WHERE cls = 'self'),
               count(*) FILTER (WHERE cls = 'strip'),
               count(*) FILTER (WHERE cls = 'drop'),
               count(*) FILTER (WHERE live_kinds > 1)
          INTO new_rules, n_self, n_strip, n_drop, n_amb
          FROM decided;

        ambiguous_left := ambiguous_left + n_amb;
        IF n_amb > 0 THEN
            -- Не трогается, но и не замалчивается: выбор цели за арендатора —
            -- отдельное решение с отдельным предметом.
            RAISE NOTICE 'security_groups %: % rule(s) carry TWO targets — left untouched, not decided here',
                g.id, n_amb;
        END IF;

        IF new_rules <> g.rules THEN
            UPDATE kacho_vpc.security_groups SET rules = new_rules WHERE id = g.id;
            rows_rewritten := rows_rewritten + 1;
            self_targeted  := self_targeted + n_self;
            keys_stripped  := keys_stripped + n_strip;
            rules_dropped  := rules_dropped + n_drop;
            -- Группа называется по id: правило снято необратимо, и владельцу
            -- стенда нужно знать, О ЧЁМ говорить с арендатором.
            RAISE NOTICE 'security_groups %: % rule(s) dropped (no expressible target), % re-targeted to the group itself, % key(s) stripped',
                g.id, n_drop, n_self, n_strip;
        END IF;
    END LOOP;

    RAISE NOTICE 'security_groups retired rule target normalize: examined % group(s) / % rule(s); rewritten % row(s); self-target %, key-stripped %, dropped %, ambiguous-left %',
        groups_examined, rules_examined, rows_rewritten,
        self_targeted, keys_stripped, rules_dropped, ambiguous_left;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Объём осмотренного перед необратимым снятием колонки. Печатается ВСЕГДА, в том
-- числе когда носителей значения ноль: иначе «нечего было терять» неотличимо от
-- «не смотрели».
DO $$
DECLARE
    rows_total   bigint;
    rows_carried bigint;
BEGIN
    SELECT count(*), count(*) FILTER (WHERE dhcp_options IS NOT NULL)
      INTO rows_total, rows_carried
      FROM kacho_vpc.subnets;
    RAISE NOTICE 'subnets.dhcp_options drop: examined % row(s), % carried a value (destroyed, not migrated)',
        rows_total, rows_carried;

    SELECT count(*), count(*) FILTER (WHERE route_distinguisher <> '')
      INTO rows_total, rows_carried
      FROM kacho_vpc.networks;
    RAISE NOTICE 'networks.route_distinguisher drop: examined % row(s), % carried a value (destroyed, not migrated)',
        rows_total, rows_carried;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_vpc.subnets DROP COLUMN IF EXISTS dhcp_options;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_vpc.networks DROP COLUMN IF EXISTS route_distinguisher;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- Down возвращает КОЛОНКИ — с тем же типом и той же обнуляемостью, — но не
-- значения: снятые значения восстановить неоткуда, как и снятые правила. Он
-- существует, чтобы неисполнимый откат не заклинил всю цепочку, а не чтобы
-- обещать обратимость.
--
-- Порядковое место колонки в таблице после Down иное (снятая и добавленная заново
-- колонка встаёт последней). Ни один путь чтения на него не опирается: и
-- репозиторий, и эта миграция перечисляют колонки ИМЕНАМИ, `SELECT *` в дереве
-- сервиса нет.
ALTER TABLE kacho_vpc.subnets  ADD COLUMN IF NOT EXISTS dhcp_options        jsonb;
ALTER TABLE kacho_vpc.networks ADD COLUMN IF NOT EXISTS route_distinguisher text NOT NULL DEFAULT '';
-- +goose StatementEnd
