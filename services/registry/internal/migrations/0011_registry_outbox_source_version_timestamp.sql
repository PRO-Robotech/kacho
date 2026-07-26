-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- source_version переезжает с BIGSERIAL id строки (миграция 0002) на clock_timestamp().
--
-- ЗАЧЕМ. Маркер обязан быть СРАВНИМ с тем, что штампует ВТОРОЙ путь доставки той же
-- регистрации — синхронный registrar, вызываемый после commit'а writer-tx. Он живёт в
-- процессе сервиса, у него нет outbox-id, и он физически не может назвать чужой
-- BIGSERIAL; kacho-iam хранит маркер в `resource_mirror.source_version timestamptz`, а
-- на проводе это `google.protobuf.Timestamp`. Пока registry штамповал число, ни один из
-- двух путей маркер не доставлял: applier его даже не декодировал (в
-- domain.RegisterIntent поля не было), sync-registrar не слал вовсе. Обе доставки
-- приходили в iam как '-infinity', то есть:
--
--   * gate повторной доставки в iam требует ПОЛОЖИТЕЛЬНОГО доказательства редоставки —
--     версии, которую можно сравнить, — и при её отсутствии открывается в сторону
--     работы (иначе непроверяемое «0 строк» задавило бы РЕАЛЬНУЮ материализацию). Без
--     версии registry не гейтился НИКОГДА и платил за обе доставки;
--   * ветка ON CONFLICT DO UPDATE зеркала гейтится `source_version < EXCLUDED.source_version`,
--     а '-infinity' < '-infinity' ЛОЖНО → строка зеркала registry писалась один раз при
--     INSERT и больше НИКОГДА не обновлялась: снятая с реестра метка не отзывала
--     label-scoped доступ (RegisterIntentForUpdate доезжал в никуда).
--
-- ПОЧЕМУ clock_timestamp(), а НЕ now(). Миграция 0002 ушла от `to_jsonb(now())` потому,
-- что now() == transaction_timestamp() фиксируется на BEGIN, а не на commit'е: два
-- конкурентных Update-воркера одного реестра, сериализованных row-lock'ом на registries,
-- могли получить маркеры в ОБРАТНОМ commit-порядку. clock_timestamp() читает часы в
-- момент срабатывания триггера, то есть на INSERT'е — а INSERT воркера, захватившего
-- row-lock вторым, физически исполняется после commit'а первого. То же и для repo-
-- intent'ов: emitRepoIntent берёт pg_advisory_xact_lock(hashtext(resource_id)) ПЕРЕД
-- INSERT'ом. Свойство монотонности из 0002 сохранено целиком — заменена только шкала.
--
-- Это ровно та форма маркера, что уже у vpc/storage/compute/nlb (эмиттер штампует
-- источник времени БД в payload внутри writer-tx, applier прокидывает его в
-- RegisterResource, sync-registrar штампует wall-clock после commit'а) — registry
-- приводится к ней, а не заводит свою.
--
-- СОВМЕСТИМОСТЬ. Строки, поставленные в очередь ДО этой миграции, несут под
-- `source_version` число. Декодер registry (domain.SourceVersion.UnmarshalJSON) читает
-- такое значение как «версии нет» → на проводе nil → iam '-infinity' → применяется
-- безусловно, ровно как сегодня. Ни одна durable-строка не отравляется.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_registry.registry_outbox_stamp_source_version() RETURNS trigger
  LANGUAGE plpgsql AS $$
BEGIN
  NEW.payload := jsonb_set(NEW.payload, '{source_version}', to_jsonb(clock_timestamp()));
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_registry.registry_outbox_stamp_source_version() RETURNS trigger
  LANGUAGE plpgsql AS $$
BEGIN
  NEW.payload := jsonb_set(NEW.payload, '{source_version}', to_jsonb(NEW.id));
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
