-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Шлюз трансляции получает ВНЕШНИЙ АДРЕС — и все четыре свойства держит база.
-- =============================================================================
-- Шлюз трансляции без внешнего адреса не делает того, ради чего существует: вид
-- и якорь размещения у него были (0030), а транслировать было не во что.
--
-- Свойства, и чем каждое held. Ни одно из них не оставлено проверке перед
-- записью: под READ COMMITTED «прочитал → записал» проигрывает параллельному
-- писателю (ban #10), а тут предмет — аренда из ОГРАНИЧЕННОГО пула, где второй
-- победитель означает два шлюза на одном адресе.
--
-- 1. gateways.external_address_id — ССЫЛКА на ресурс адреса, а не строка.
--    FK ON DELETE RESTRICT: адрес не исчезает из-под живого шлюза. Обратную
--    сторону привязки — ту, которую видит ВЛАДЕЛЕЦ адреса, — держит строка
--    `address_references` (referrer_type='vpc_gateway'), поэтому `Address.used_by`
--    называет шлюз, а `Address.used` = true. Две записи об одном факте пишутся
--    ОДНИМ вызывающим в одной writer-TX; расхождение ловит интеграционная проба
--    TestIntegration_Gateway_ExternalAddress_BindingIsVisibleToAddressOwner.
--
-- 2. Один адрес — не более чем один шлюз. Частичный UNIQUE по
--    external_address_id. Заметим, что `address_references.address_id` — PRIMARY
--    KEY, то есть у адреса не может быть двух referrer'ов вообще; здесь тот же
--    инвариант закрывается со стороны шлюза, потому что колонка и строка ссылки
--    суть разные таблицы и обе обязаны быть однозначны сами по себе.
--
-- 3. Адрес держит ТОЛЬКО вид NAT. У вида «только исход» (IPv6) публичного адреса
--    нет by design — отсутствие входящей достижимости и есть смысл этого вида,
--    поэтому у его ветви контракта нет поля адреса. CHECK делает то же на уровне
--    строки: ветвь oneof в БД не представлена, и без него «egress-only с адресом»
--    было бы записываемым состоянием.
--
-- 4. Вид NAT ОБЯЗАН нести адрес. Это и есть закрываемый дефект, выраженный
--    инвариантом строки, а не обещанием кода: биусловие ниже делает состояние
--    «шлюз трансляции без адреса» НЕзаписываемым.
--
-- ЧТО ДЕЛАЕТСЯ С УЖЕ СУЩЕСТВУЮЩИМИ СТРОКАМИ — и почему НЕ то, что в 0030.
-- 0030 снимала легаси-строки, потому что у них не было представления в новом
-- контракте (ветвь oneof снята, якоря взять негде). Здесь случай ДРУГОЙ:
-- легаси-строка представима полностью, ей лишь нечего показать в новом поле.
-- Снимать её было бы разрушением того, что работает, и вдобавок миной под накат:
-- на `gateways` ссылается `route_table_gateway_refs` с ON DELETE RESTRICT, то
-- есть DELETE легаси-шлюза, названного живым маршрутом, уронил бы саму миграцию —
-- на чистой БД такая ветвь зелёная и до боевого наката её никто не увидит.
--
-- Поэтому биусловие вводится как NOT VALID: оно связывает КАЖДУЮ строку,
-- записываемую с этого момента (INSERT и UPDATE проверяются сразу), и не
-- предъявляет требования задним числом. Затем — попытка признать его полным,
-- обусловленная ПЕРЕСЧЁТОМ: легаси-строк нет → VALIDATE, и ограничение
-- становится полным; легаси-строки есть → число ПЕЧАТАЕТСЯ, чтобы «ноль» было
-- отличимо от «не смотрели», а накат не падал. На сегодняшнем дереве обе ветви
-- достижимы, и обе оставляют инвариант действующим для новых записей.
--
-- Идемпотентно (IF NOT EXISTS / DO-guard по pg_constraint) — повторный или
-- параллельный migrate-init при helm rollout, как 0016/0024/0028/0030.

SET search_path TO kacho_vpc, public;

ALTER TABLE kacho_vpc.gateways ADD COLUMN IF NOT EXISTS external_address_id text;

DO $$
DECLARE
    legacy_nat bigint;
BEGIN
    -- 1. Ссылка на ресурс адреса.
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'gateways_external_address_fk'
           AND conrelid = 'kacho_vpc.gateways'::regclass
    ) THEN
        ALTER TABLE kacho_vpc.gateways
            ADD CONSTRAINT gateways_external_address_fk
            FOREIGN KEY (external_address_id)
            REFERENCES kacho_vpc.addresses(id) ON DELETE RESTRICT;
    END IF;

    -- 3. Адрес держит только вид NAT.
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'gateways_external_address_kind_chk'
           AND conrelid = 'kacho_vpc.gateways'::regclass
    ) THEN
        ALTER TABLE kacho_vpc.gateways
            ADD CONSTRAINT gateways_external_address_kind_chk
            CHECK (external_address_id IS NULL OR gateway_type = 'NAT');
    END IF;

    -- 4. Вид NAT обязан нести адрес — для всего, что пишется с этого момента.
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'gateways_nat_has_address_chk'
           AND conrelid = 'kacho_vpc.gateways'::regclass
    ) THEN
        ALTER TABLE kacho_vpc.gateways
            ADD CONSTRAINT gateways_nat_has_address_chk
            CHECK (gateway_type <> 'NAT' OR external_address_id IS NOT NULL)
            NOT VALID;
    END IF;

    SELECT count(*) INTO legacy_nat
      FROM kacho_vpc.gateways
     WHERE gateway_type = 'NAT' AND external_address_id IS NULL;

    RAISE NOTICE 'kacho_vpc.gateways: шлюзов трансляции без внешнего адреса (легаси): %', legacy_nat;

    IF legacy_nat = 0 THEN
        ALTER TABLE kacho_vpc.gateways VALIDATE CONSTRAINT gateways_nat_has_address_chk;
        RAISE NOTICE 'kacho_vpc.gateways: биусловие «NAT ⇔ есть адрес» признано полным';
    END IF;
END
$$;

-- 2. Один адрес — не более чем один шлюз.
CREATE UNIQUE INDEX IF NOT EXISTS gateways_external_address_uniq
    ON kacho_vpc.gateways (external_address_id)
 WHERE external_address_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

DROP INDEX IF EXISTS kacho_vpc.gateways_external_address_uniq;

ALTER TABLE kacho_vpc.gateways
    DROP CONSTRAINT IF EXISTS gateways_nat_has_address_chk,
    DROP CONSTRAINT IF EXISTS gateways_external_address_kind_chk,
    DROP CONSTRAINT IF EXISTS gateways_external_address_fk;

ALTER TABLE kacho_vpc.gateways DROP COLUMN IF EXISTS external_address_id;

-- +goose StatementEnd
