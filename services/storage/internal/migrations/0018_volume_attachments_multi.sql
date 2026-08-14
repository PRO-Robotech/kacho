-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0018: первичный ключ привязки — пара (том, инстанс).
--
-- WHY. Инвариант «том привязан не более чем к одному инстансу» был выражен ФОРМОЙ
-- ключа: первичным ключом стоял один volume_id. Это верно ровно до тех пор, пока
-- множественная привязка не поддерживается ни одним бэкендом. Но поддерживается она
-- или нет — свойство БЭКЕНДА, а не нашей схемы: у блочного хранилища это вопрос
-- режима исключительной блокировки, и разные бэкенды отвечают на него по-разному.
--
-- Поэтому ключ описывает ФОРМУ факта («эта привязка — про этот том и этот инстанс»),
-- а ограничение «не более одной» переезжает в предикат атомарной вставки, который
-- читает способность действующей ревизии привязки класса. Ограничение остаётся на
-- уровне БД — оно исполняется тем же единственным стейтментом, что и запись, и
-- обойти его гонкой нельзя.
--
-- Смена первичного ключа — операция на живой таблице, поэтому делается СЕЙЧАС, а не
-- когда множественная привязка понадобится: тогда она стоила бы миграции под
-- нагрузкой ради того, что можно получить бесплатно на пустой таблице.
--
-- Что НЕ меняется: ограничительная связь на том (привязанный том не удаляется),
-- уникальность имени устройства в пределах инстанса и запрет второго загрузочного
-- тома. Все три остаются ровно теми же.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

ALTER TABLE kacho_storage.volume_attachments
    DROP CONSTRAINT volume_attachments_pkey;

ALTER TABLE kacho_storage.volume_attachments
    ADD CONSTRAINT volume_attachments_pkey PRIMARY KEY (volume_id, instance_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

-- Возврат к одиночной привязке возможен только если множественных строк нет:
-- иначе прежний ключ неисполним, и молча выбрасывать чужие привязки нельзя.
DELETE FROM kacho_storage.volume_attachments a
 WHERE EXISTS (SELECT 1 FROM kacho_storage.volume_attachments b
                WHERE b.volume_id = a.volume_id AND b.ctid < a.ctid);

ALTER TABLE kacho_storage.volume_attachments
    DROP CONSTRAINT volume_attachments_pkey;

ALTER TABLE kacho_storage.volume_attachments
    ADD CONSTRAINT volume_attachments_pkey PRIMARY KEY (volume_id);
-- +goose StatementEnd
