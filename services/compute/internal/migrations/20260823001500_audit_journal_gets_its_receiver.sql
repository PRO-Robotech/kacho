-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- У журнала аудита появился приёмник: учётные колонки приводятся к доставке.
-- =============================================================================
-- Задача `PRO-Robotech/kacho#812`. Брат миграции службы прав того же номера:
-- приёмник журнала на платформе один, поэтому и форма учётных колонок у обоих
-- журналов обязана быть одна — иначе один и тот же вопрос про доставку
-- задавался бы двум таблицам по-разному.
--
-- ЧТО ИЗМЕНИЛОСЬ. Журнал вычислений перестал быть очередью без адресата:
-- строки вывозятся в поток структурных записей службы и помечаются
-- доставленными.
--
-- ПОЧЕМУ БАЗУ НАДО ТРОГАТЬ. Причина отказа приёмника хранилась негде: строка
-- умела сказать «не доставлена», но не умела сказать, почему. Колонка заводится
-- здесь.
--
-- ПОЧЕМУ СЛОВАРЬ СОСТОЯНИЙ СУЖАЕТСЯ ДО ДВУХ. Он становится ровно таким, каким
-- его производит код: `'pending'` и `'sent'`. Двух других не пишет никто и
-- никогда не писал — «полёта» не существует, потому что строка держится
-- блокировкой своей транзакции от клейма до пометки (срыв процесса откатывает
-- клейм целиком), а терминального отказа не существует, потому что у приёмника
-- нет класса «не приму никогда»: единственная непринимаемая запись — та, у
-- которой нет идентификатора или глагола, а такой строки не бывает, обе колонки
-- закрыты формой. Держать состояние, которого продукт произвести не умеет,
-- значит обещать подсистему, которой нет.
--
-- ЧТО ДЕЛАЕТСЯ СО СТАРЫМИ СТРОКАМИ. Ничего, кроме приведения состояния: строки
-- остаются недоставленными и вывозятся обычным порядком. Оператор приведения к
-- `'pending'` рассчитан на НОЛЬ строк и стоит здесь затем, чтобы сужение
-- ограничения не отказало на строке, о которой мы не знаем.
--
-- ИНДЕКС. Вывоз обходит очередь В ПОРЯДКЕ СОЗДАНИЯ, поэтому индекс
-- перестраивается так, чтобы порядок брался из него, а не из сортировки всей
-- накопленной головы.

-- +goose Up

UPDATE audit_outbox
   SET status = 'pending'
 WHERE status NOT IN ('pending', 'sent');

ALTER TABLE audit_outbox
    DROP CONSTRAINT IF EXISTS audit_outbox_status_check;

ALTER TABLE audit_outbox
    ADD CONSTRAINT audit_outbox_status_check
        CHECK (status IN ('pending', 'sent'));

ALTER TABLE audit_outbox
    ADD COLUMN IF NOT EXISTS last_error text;

DROP INDEX IF EXISTS audit_outbox_pending_idx;

CREATE INDEX IF NOT EXISTS audit_outbox_pending_idx
    ON audit_outbox (created_at, id)
    WHERE status <> 'sent';

-- +goose Down

DROP INDEX IF EXISTS audit_outbox_pending_idx;

CREATE INDEX audit_outbox_pending_idx
    ON audit_outbox (next_attempt_at, id)
    WHERE status <> 'sent';

ALTER TABLE audit_outbox
    DROP COLUMN IF EXISTS last_error;

ALTER TABLE audit_outbox
    DROP CONSTRAINT IF EXISTS audit_outbox_status_check;

ALTER TABLE audit_outbox
    ADD CONSTRAINT audit_outbox_status_check
        CHECK (status IN ('pending', 'in_flight', 'sent', 'failed'));
