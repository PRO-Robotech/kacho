-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0019: снимок и образ несут СВОЁ пространство арендатора у бэкенда.
--
-- WHY. Миграция 0017 завела координату объекта на трёх ресурсах, но пространство
-- арендатора — только у тома. Снимок и образ оказались адресуемы лишь наполовину:
-- имя объекта у них есть, а пространства, в котором это имя что-то значит, нет.
--
-- Достроить координату «на чтении», через источник, было бы возвратом того самого
-- класса, который в этой же ветке закрывался зоной снимка: источник обнуляется при
-- удалении (снимок обязан его переживать), и вместе с ним исчезала бы координата
-- живого объекта — уже не проверка вырождалась бы, а адресация. Объект у бэкенда
-- существует независимо от строки источника, значит и адрес обязан храниться у того,
-- кому он принадлежит.
--
-- Найдено интеграционной пробой полного цикла: обход сверщика падал на снимке и
-- образе с «колонки не существует», при том что по тому проходил. Ровно та разница,
-- которую разбор кода не показывает.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

ALTER TABLE kacho_storage.snapshots ADD COLUMN backend_namespace text NOT NULL DEFAULT '';
ALTER TABLE kacho_storage.images    ADD COLUMN backend_namespace text NOT NULL DEFAULT '';

-- Существующие строки берут пространство у источника ТАМ, ГДЕ ОН ЕЩЁ ЖИВ. Где
-- источника уже нет, пространство остаётся пустым — и это честное «координата
-- неизвестна», а не выдуманное значение: строка досталась от схемы, в которой
-- адреса не было вовсе.
UPDATE kacho_storage.snapshots s
   SET backend_namespace = v.backend_namespace
  FROM kacho_storage.volumes v
 WHERE v.id = s.source_volume_id AND s.backend_namespace = '';

UPDATE kacho_storage.images i
   SET backend_namespace = COALESCE(sn.backend_namespace, v.backend_namespace, '')
  FROM kacho_storage.snapshots sn
  FULL JOIN kacho_storage.volumes v ON false
 WHERE (sn.id = i.source_snapshot_id OR v.id = i.source_volume_id)
   AND i.backend_namespace = '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

ALTER TABLE kacho_storage.images    DROP COLUMN IF EXISTS backend_namespace;
ALTER TABLE kacho_storage.snapshots DROP COLUMN IF EXISTS backend_namespace;
-- +goose StatementEnd
