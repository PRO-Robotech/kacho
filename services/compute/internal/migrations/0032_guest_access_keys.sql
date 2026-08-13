-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- Ключи, с которыми арендатор входит в свои машины.
--
-- ТОЛЬКО ПУБЛИЧНАЯ ПОЛОВИНА. Закрытая не покидает машину арендатора и здесь не
-- хранится никогда: хранилище закрытых ключей — отдельный домен со своей
-- моделью угроз, и принять её сюда значило бы сделать эту базу целью, которой
-- она быть не должна.
--
-- ПОЧЕМУ ОТДЕЛЬНЫЙ РЕСУРС, А НЕ ПОЛЕ МАШИНЫ. Ключ, живущий полем, существует
-- ровно столько, сколько машина: его нельзя ни отозвать, ни заменить, ни
-- узнать, где ещё он используется. Это разница между «ключ у нас есть» и «мы
-- знаем, кто может войти».

-- +goose Up

CREATE TABLE guest_access_keys (
    id           text PRIMARY KEY,
    project_id   text NOT NULL,

    -- Косметическое имя: меняется свободно, в ссылках не участвует. Уникально в
    -- пределах проекта — два ключа с одним именем неразличимы для человека,
    -- который выбирает, какой снять.
    name         text NOT NULL,

    -- Материал публичного ключа в форме, понятной гостевой системе.
    public_key   text NOT NULL,

    -- Отпечаток, вычисленный НАМИ. Служит сверке: арендатор сравнивает его с
    -- тем, что видит у себя, и узнаёт, тот ли ключ доехал.
    --
    -- Уникален глобально: один и тот же материал, заведённый дважды, — это не
    -- два ключа, а один, про который забыли. Разные проекты при этом вправе
    -- иметь одинаковый ключ, поэтому уникальность в пределах проекта, а не
    -- облака: глобальная запретила бы двум арендаторам пользоваться одним
    -- рабочим ключом, что не наше дело.
    fingerprint  text NOT NULL,

    labels       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT guest_access_keys_id_check
        CHECK (id ~ '^gak-[0-9a-hjkmnp-tv-z]{17}$'),
    CONSTRAINT guest_access_keys_name_check
        CHECK (length(name) BETWEEN 1 AND 63),
    CONSTRAINT guest_access_keys_public_key_check
        CHECK (length(public_key) BETWEEN 1 AND 16384),
    CONSTRAINT guest_access_keys_fingerprint_check
        CHECK (length(fingerprint) BETWEEN 1 AND 128),
    CONSTRAINT guest_access_keys_labels_object_check
        CHECK (jsonb_typeof(labels) = 'object')
);

-- Имя различает ключи для человека — значит оно обязано быть различающим.
CREATE UNIQUE INDEX guest_access_keys_project_name_uniq
    ON guest_access_keys (project_id, name);

-- Один и тот же материал в одном проекте — это один ключ, про который забыли.
CREATE UNIQUE INDEX guest_access_keys_project_fingerprint_uniq
    ON guest_access_keys (project_id, fingerprint);

-- Курсор списка: та же пара, что у прочих ресурсов продукта.
CREATE INDEX guest_access_keys_cursor_idx
    ON guest_access_keys (project_id, created_at, id);

-- Привязка ключей к машине.
--
-- ОТДЕЛЬНАЯ ТАБЛИЦА, А НЕ МАССИВ В СТРОКЕ МАШИНЫ: связь многие-ко-многим, и
-- массив не даёт ни ссылочной целостности, ни ответа на вопрос «где ещё этот
-- ключ используется» — а именно он делает снятие осмысленным.
CREATE TABLE instance_guest_access_keys (
    instance_id          text NOT NULL
        REFERENCES instances(id) ON DELETE CASCADE,

    -- Снятие ключа, привязанного к живой машине, ЗАПРЕЩЕНО ссылочной
    -- целостностью, а не проверкой в коде: проверка защищает только тот путь,
    -- который через неё проходит, и снятие мимо неё оставило бы машину со
    -- ссылкой в никуда.
    guest_access_key_id  text NOT NULL
        REFERENCES guest_access_keys(id) ON DELETE RESTRICT,

    attached_at          timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (instance_id, guest_access_key_id)
);

CREATE INDEX instance_guest_access_keys_by_key_idx
    ON instance_guest_access_keys (guest_access_key_id);

-- +goose Down

DROP TABLE IF EXISTS instance_guest_access_keys;
DROP TABLE IF EXISTS guest_access_keys;
