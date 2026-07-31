-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Область значений правила SecurityGroup — конструкцией базы.
-- =============================================================================
-- Порты и протокол правила проверяются синхронно в use-case
-- (`validateSGRulePorts` / `validateSGRuleProtocol`), но синхронная проверка
-- ограничивает ОДИН запрос вызывающего. Инвариант «правило выразимо продуктом»
-- обязан жить конструкцией базы: она отвечает КАЖДОМУ писателю, включая тех,
-- кто приходит мимо use-case, — inline-путь default-SG в writer-TX создания
-- сети, будущий воркер, восстановление из дампа, ручной SQL.
--
-- Границы держать в синхроне с domain/security_group_protocol.go:
--   AnyPort=-1, MinPort=0, MaxPort=65535,
--   AnyProtocolNumber=-1, MinProtocolNumber=0, MaxProtocolNumber=255,
--   набор имён — KnownProtocolNames() (150 значений: 'any' + ключевые слова
--   реестра IANA + принятые псевдонимы).
-- Паритет наборов ДОКАЗЫВАЕТСЯ, а не заявляется: гейт
-- `TestSGRulesDomain_ProtocolNameSetParityWithCode` перечисляет ОБА набора
-- (`domain.KnownProtocolNames` и `kacho_sg_protocol_names()`), сверяет их как
-- множества в ОБЕ стороны и утверждает объём осмотренного числом. Проверен
-- инъекцией в обе стороны: лишнее имя в наборе базы даёт «база принимает имена,
-- которых нет в коде»; согласованные наборы гейт проходит молча.
--
-- Обратное заполнение ниже удаляет данные необратимо, и счётчик пройденных
-- строк здесь — ПЕЧАТЬ, а не утверждение: манифест `dropguard.json` считает
-- ТАБЛИЦЫ и правку внутри JSONB выразить не может. Основание, на которое это
-- опирается, названо явно: директива владельца 2026-07-27 «облако не в проде,
-- тенантских данных нет». Перестанет быть верным — число обязано стать
-- утверждением, а не остаться NOTICE.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- kacho_sg_protocol_names — сам набор, отдельной функцией, а не литералом внутри
-- предиката. Иначе набор базы можно только ОПРАШИВАТЬ по одному имени, а
-- опрос доказывает лишь «код ⊆ база»: имя, добавленное сюда и отсутствующее в
-- коде, сделало бы ограничение ШИРЕ продукта и осталось бы незамеченным. Гейт
-- паритета перечисляет обе стороны и сверяет их как множества.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_sg_protocol_names()
RETURNS text[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $fn$
    SELECT ARRAY[
        '3pc', 'a/n', 'aggfrag', 'ah', 'all', 'any', 'argus', 'aris',
        'ax.25', 'bbn-rcc-mon', 'bit-emu', 'bna', 'br-sat-mon', 'cbt',
        'cftp', 'chaos', 'compaq-peer', 'cphb', 'cpnx', 'crtp', 'crudp',
        'dccp', 'dcn-meas', 'ddp', 'ddx', 'dgp', 'dsr', 'egp', 'eigrp',
        'emcon', 'encap', 'esp', 'etherip', 'ethernet', 'fc', 'fire',
        'ggp', 'gmtp', 'gre', 'hip', 'hmp', 'homa', 'hopopt', 'i-nlsp',
        'iatp', 'icmp', 'icmpv6', 'idpr', 'idpr-cmtp', 'idrp', 'ifmp',
        'igmp', 'igp', 'il', 'ipcomp', 'ipcv', 'ipencap', 'ipip', 'iplt',
        'ippc', 'iptm', 'ipv4', 'ipv6', 'ipv6-frag', 'ipv6-icmp',
        'ipv6-nonxt', 'ipv6-opts', 'ipv6-route', 'ipx-in-ip', 'irtp',
        'isis', 'iso-ip', 'iso-tp4', 'kryptolan', 'l2tp', 'larp', 'leaf-1',
        'leaf-2', 'manet', 'merit-inp', 'mfe-nsp', 'micp', 'min-ipv4',
        'mobile', 'mobility-header', 'mpls-in-ip', 'mtp', 'mux', 'narp',
        'netblt', 'nsfnet-igp', 'nsh', 'nvp-ii', 'ospf', 'ospfigp', 'pgm',
        'pim', 'pipe', 'pnni', 'prm', 'ptp', 'pup', 'pvp', 'qnx', 'rdp',
        'rohc', 'rsvp', 'rsvp-e2e-ignore', 'rvd', 'sat-expak', 'sat-mon',
        'scc-sp', 'scps', 'sctp', 'sdrp', 'secure-vmtp', 'shim6', 'skip',
        'sm', 'smp', 'snp', 'sprite-rpc', 'sps', 'srp', 'sscopmce', 'st',
        'stp', 'sun-nd', 'swipe', 'tcf', 'tcp', 'tlsp', 'tp++', 'trunk-1',
        'trunk-2', 'ttp', 'udp', 'udplite', 'uti', 'vines', 'visa', 'vmtp',
        'vrrp', 'wb-expak', 'wb-mon', 'wesp', 'wsn', 'xnet', 'xns-idp',
        'xtp'
    ]::text[]
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
-- kacho_sg_protocol_name_valid — членство имени в наборе, который продукт умеет
-- выразить. Регистр не важен (реестр печатает имена заглавными, клиенты пишут
-- строчными). Пустая строка сюда НЕ передаётся: «протокол не задан» решает
-- вызывающий предикат, ровно как в domain.IsKnownProtocolName.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_sg_protocol_name_valid(proto_name text)
RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $fn$
    SELECT lower(proto_name) = ANY (kacho_vpc.kacho_sg_protocol_names())
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
-- kacho_sg_rule_expressible — одно правило: диапазон портов и протокол.
--
-- Функция НИКОГДА не возбуждает исключение: и неожиданный JSON-ТИП, и число,
-- которое не является целым нужного размера (`1.5`, `1.0`, `1e999`), отвечают
-- false, а не ошибкой. Ограничение, падающее исключением вместо 23514, вернуло
-- бы вызывающему ошибку СОВСЕМ другого класса (разбор числа, `22P02`/`22003`),
-- которую маппер репозитория относит к разбору идентификатора, а обратное
-- заполнение ниже на такой строке просто прервалось бы. Поэтому число
-- проверяется РЕГУЛЯРНЫМ ВЫРАЖЕНИЕМ до приведения типа: `::bigint` в plpgsql
-- перехватить нечем, не перейдя на EXCEPTION-блок, а он несовместим с
-- IMMUTABLE-семантикой, на которую опирается CHECK.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_sg_rule_expressible(rule jsonb)
RETURNS boolean
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $fn$
DECLARE
    from_port  bigint;
    to_port    bigint;
    proto_name text;
    proto_num  bigint;
BEGIN
    IF rule IS NULL OR jsonb_typeof(rule) <> 'object' THEN
        RETURN false;
    END IF;

    -- Отсутствующий/JSON-null ключ = нулевое значение Go-структуры: правило
    -- записано без этого поля, и это законно. `jsonb_typeof` отсутствующего
    -- ключа — SQL NULL, поэтому он сводится COALESCE'ом: ветка `WHEN NULL` в
    -- CASE не сработала бы никогда (NULL = NULL неизвестно), и отсутствующий
    -- ключ молча уехал бы в ELSE, то есть в отказ.
    -- `^-?\d{1,9}$` — целое без дробной части и экспоненты, заведомо влезающее
    -- в bigint. Диапазоны полей (`0-65535`, `-1..255`) уже, поэтому сужение до
    -- девяти цифр ничего выразимого не отсекает: всё, что длиннее, всё равно
    -- вне границ и обязано ответить false.
    from_port := CASE
                     WHEN jsonb_typeof(rule -> 'FromPort') IS NULL THEN 0
                     WHEN jsonb_typeof(rule -> 'FromPort') = 'null' THEN 0
                     WHEN jsonb_typeof(rule -> 'FromPort') = 'number'
                          AND (rule ->> 'FromPort') ~ '^-?[0-9]{1,9}$'
                          THEN (rule ->> 'FromPort')::bigint
                     ELSE NULL END;
    to_port   := CASE
                     WHEN jsonb_typeof(rule -> 'ToPort') IS NULL THEN 0
                     WHEN jsonb_typeof(rule -> 'ToPort') = 'null' THEN 0
                     WHEN jsonb_typeof(rule -> 'ToPort') = 'number'
                          AND (rule ->> 'ToPort') ~ '^-?[0-9]{1,9}$'
                          THEN (rule ->> 'ToPort')::bigint
                     ELSE NULL END;
    IF from_port IS NULL OR to_port IS NULL THEN
        RETURN false;
    END IF;

    -- `-1` — собственное написание продукта «любой порт», принимается ТОЛЬКО на
    -- обеих границах сразу (§23 07-known-divergences). Полудиапазон «от любого
    -- до 80» — не диапазон, а два разных утверждения в одном поле.
    IF from_port = -1 OR to_port = -1 THEN
        IF from_port <> -1 OR to_port <> -1 THEN
            RETURN false;
        END IF;
    ELSE
        IF from_port < 0 OR from_port > 65535 OR to_port < 0 OR to_port > 65535 THEN
            RETURN false;
        END IF;
        IF from_port > to_port THEN
            RETURN false;
        END IF;
    END IF;

    proto_name := CASE COALESCE(jsonb_typeof(rule -> 'ProtocolName'), 'absent')
                      WHEN 'string' THEN rule ->> 'ProtocolName'
                      WHEN 'null'   THEN ''
                      WHEN 'absent' THEN ''
                      ELSE NULL END;
    IF proto_name IS NULL THEN
        RETURN false;
    END IF;
    IF proto_name <> '' AND NOT kacho_vpc.kacho_sg_protocol_name_valid(proto_name) THEN
        RETURN false;
    END IF;

    proto_num := CASE
                     WHEN jsonb_typeof(rule -> 'ProtocolNumber') IS NULL THEN 0
                     WHEN jsonb_typeof(rule -> 'ProtocolNumber') = 'null' THEN 0
                     WHEN jsonb_typeof(rule -> 'ProtocolNumber') = 'number'
                          AND (rule ->> 'ProtocolNumber') ~ '^-?[0-9]{1,9}$'
                          THEN (rule ->> 'ProtocolNumber')::bigint
                     ELSE NULL END;
    IF proto_num IS NULL THEN
        RETURN false;
    END IF;
    IF proto_num <> -1 AND (proto_num < 0 OR proto_num > 255) THEN
        RETURN false;
    END IF;

    RETURN true;
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
-- kacho_sg_rules_domain_valid — весь набор правил колонки.
--
-- Значение колонки бывает JSON-скаляром `null` (маршалинг Go nil-slice) — это
-- отсутствие набора, а не набор с невыразимым правилом; тот же приём, что в
-- security_groups_rules_cardinality (0024).
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_sg_rules_domain_valid(rules jsonb)
RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $fn$
    SELECT rules IS NULL
        OR jsonb_typeof(rules) <> 'array'
        OR NOT EXISTS (
            SELECT 1 FROM jsonb_array_elements(rules) AS r
             WHERE NOT kacho_vpc.kacho_sg_rule_expressible(r)
        )
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Обратное заполнение строк, записанных ДО того, как область значений стала
-- проверяться (в проверке её не было ни в use-case, ни в доменной модели, ни в
-- базе — принималось что угодно).
--
-- Без него `ALTER TABLE ADD CONSTRAINT` ниже не применился бы вовсе: он
-- валидирует существующие строки. А применившись на «чистой» базе и упав на
-- «грязной», он сделал бы такие группы неисправимыми через API — любая
-- последующая запись строки (переименование соседнего правила, добавление или
-- удаление другого) отбивалась бы ограничением.
--
-- Правило, которое продукт выразить не может, УДАЛЯЕТСЯ из набора. Правила
-- группы — разрешающие (`Allows ingress/egress traffic`), поэтому удаление
-- разрешающего правила доступ не расширяет. Любая иная нормализация придумала
-- бы намерение: подтянуть 65536 к 65535 значит выбрать за вызывающего порт, а
-- заменить несуществующий протокол на «любой» — РАСШИРИТЬ правило до всех
-- протоколов. Обмен назван осознанно: невыразимое правило исчезает из проекции
-- ресурса, а не остаётся строкой, которую нельзя ни применить, ни отредактировать.
--
-- Действие необратимо (см. Down): удалённые правила восстановить неоткуда.
DO $$
DECLARE
    affected_rows  bigint;
    dropped_rules  bigint;
BEGIN
    SELECT count(*), COALESCE(sum(bad), 0) INTO affected_rows, dropped_rules
      FROM (
        SELECT (SELECT count(*)
                  FROM jsonb_array_elements(s.rules) AS r
                 WHERE NOT kacho_vpc.kacho_sg_rule_expressible(r)) AS bad
          FROM kacho_vpc.security_groups s
         WHERE jsonb_typeof(s.rules) = 'array'
           AND NOT kacho_vpc.kacho_sg_rules_domain_valid(s.rules)
      ) t;

    UPDATE kacho_vpc.security_groups s
       SET rules = COALESCE(
               (SELECT jsonb_agg(r.value ORDER BY r.ord)
                  FROM jsonb_array_elements(s.rules) WITH ORDINALITY AS r(value, ord)
                 WHERE kacho_vpc.kacho_sg_rule_expressible(r.value)),
               '[]'::jsonb)
     WHERE jsonb_typeof(s.rules) = 'array'
       AND NOT kacho_vpc.kacho_sg_rules_domain_valid(s.rules);

    -- Число печатается всегда: «ноль затронутых строк» обязано быть отличимо
    -- от «заполнение не выполнялось».
    RAISE NOTICE 'security_groups rules backfill: % row(s) rewritten, % unexpressible rule(s) dropped',
        affected_rows, dropped_rules;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Идемпотентность — DO-guard по pg_constraint (ALTER TABLE ADD CONSTRAINT не
-- поддерживает IF NOT EXISTS для CHECK), как в 0011/0016/0024.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'security_groups_rules_domain') THEN
        ALTER TABLE kacho_vpc.security_groups
            ADD CONSTRAINT security_groups_rules_domain
            CHECK (kacho_vpc.kacho_sg_rules_domain_valid(rules));
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- Обратное заполнение не откатывается: удалённые невыразимые правила
-- восстановить неоткуда. Down снимает только проверку.
ALTER TABLE kacho_vpc.security_groups
    DROP CONSTRAINT IF EXISTS security_groups_rules_domain;
DROP FUNCTION IF EXISTS kacho_vpc.kacho_sg_rules_domain_valid(jsonb);
DROP FUNCTION IF EXISTS kacho_vpc.kacho_sg_rule_expressible(jsonb);
DROP FUNCTION IF EXISTS kacho_vpc.kacho_sg_protocol_name_valid(text);
DROP FUNCTION IF EXISTS kacho_vpc.kacho_sg_protocol_names();

-- +goose StatementEnd
