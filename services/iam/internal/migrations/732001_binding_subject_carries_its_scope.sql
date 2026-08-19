-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- 732001_binding_subject_carries_its_scope — пара «субъект + область»
-- разрешается ОДНИМ индексом на ОДНОМ отношении.
--
-- Номер выведен из задачи (kacho#732) + порядковый разряд внутри неё.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ПРЕДМЕТ
--
-- Вопрос вердикта сужается двумя предикатами: «выдача называет ЭТОГО субъекта»
-- и «выдача сделана на область, лежащую на цепи ЭТОГО объекта». Предикаты
-- стояли на РАЗНЫХ отношениях — субъект на `access_binding_subjects`, область
-- на `access_bindings`, — и одного индекса, обслуживающего оба, не существовало
-- by construction. Следствие наблюдаемо на обеих сторонах и симметрично:
--
--   · заход со стороны области читает ВСЕ выдачи области — чужие выдачи входят
--     в стоимость чужого вердикта;
--   · заход со стороны субъекта читает ВСЕ выдачи субъекта в облаке — а их
--     число ничем не ограничено, потому что проектов в облаке неограниченно.
--
-- Индекс по одному лишь субъекту закрывает первую сторону и ОТКРЫВАЕТ вторую.
-- Поэтому область переносится на строку субъекта, и обе стороны сужаются одним
-- обращением.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЭТО ВОССТАНОВЛЕНИЕ ПАРИТЕТА, А НЕ НОВОЕ УСТРОЙСТВО
--
-- Соседнее соединение того же запроса уже читает членство ПАРОЙ КОЛОНОК и
-- обслуживается индексом `group_members_member_idx (member_type, member_id)`.
-- И на самой родительской таблице индекс `access_bindings_subject_idx
-- (subject_type, subject_id)` существует с самого начала — он покрывает
-- legacy-проекцию subjects[0]; когда миграция 0028 вынесла набор субъектов в
-- дочернюю таблицу, индекс за ним не поехал. То есть склейка и отсутствие
-- индекса — невынесенный хвост переноса, а не замысел.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- СОГЛАСОВАННОСТЬ ПЕРЕНЕСЁННЫХ КОЛОНОК — МЕХАНИЗМОМ БД (ban #10)
--
-- Переносить значение и синхронизировать его кодом значило бы завести
-- software check-then-act на инварианте внутри одного сервиса. Здесь работают
-- ДВА механизма, и каждый закрывает свою половину:
--
--   1. СОСТАВНОЙ ВНЕШНИЙ КЛЮЧ `(binding_id, resource_type, resource_id)` →
--      `access_bindings (id, resource_type, resource_id)`. Он делает
--      РАСХОЖДЕНИЕ НЕВЫРАЗИМЫМ: строка субъекта, называющая не ту область,
--      не ссылается ни на что и отвергается. `ON UPDATE CASCADE` переносит
--      правку области у родителя на детей САМ, `ON DELETE CASCADE` сохраняет
--      прежнее поведение снятия выдачи (ban #4 разрешает каскад внутри одной
--      схемы). Прежний однополосный внешний ключ им поглощён и снят: два
--      каскадных пути к одной строке — это два места об одном предмете.
--
--   2. ТРИГГЕР ЗАПОЛНЕНИЯ. Писатель строки субъекта область не называет и
--      называть не обязан — она свойство выдачи, а не субъекта. Триггер берёт
--      её у родителя ДО проверок, поэтому ни один существующий писатель
--      (включая посевы и обратные заполнения прежних миграций) не ломается, а
--      солгать всё равно нельзя: значение проверит внешний ключ.
--
-- Пустой носитель не проглатывается: выдачи-родителя нет ⇒ отказ, а не тихий
-- пропуск. Тихий пропуск оставил бы строку без области, и она выпала бы из
-- вердикта молча — то есть право перестало бы действовать без единого признака.

-- +goose Up

-- Родитель обязан быть адресуем парой «идентификатор + область»: составному
-- внешнему ключу нужна цель. Кортеж уникален тривиально (id — первичный ключ),
-- поэтому ограничение ничего не запрещает — оно объявляет цель.
ALTER TABLE kacho_iam.access_bindings
  ADD CONSTRAINT access_bindings_id_scope_uk UNIQUE (id, resource_type, resource_id);

ALTER TABLE kacho_iam.access_binding_subjects
  ADD COLUMN resource_type text,
  ADD COLUMN resource_id   text;

UPDATE kacho_iam.access_binding_subjects bs
   SET resource_type = b.resource_type,
       resource_id   = b.resource_id
  FROM kacho_iam.access_bindings b
 WHERE b.id = bs.binding_id;

ALTER TABLE kacho_iam.access_binding_subjects
  ALTER COLUMN resource_type SET NOT NULL,
  ALTER COLUMN resource_id   SET NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_iam.access_binding_subject_carries_scope()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  rt text;
  ri text;
BEGIN
  -- Названа вызывающим — берём как есть: солгать не даст внешний ключ.
  IF NEW.resource_type IS NOT NULL AND NEW.resource_id IS NOT NULL THEN
    RETURN NEW;
  END IF;

  SELECT b.resource_type, b.resource_id
    INTO rt, ri
    FROM kacho_iam.access_bindings b
   WHERE b.id = NEW.binding_id;

  IF NOT FOUND THEN
    -- Отказ, а не тихий пропуск: строка без области выпала бы из вердикта
    -- молча, и право перестало бы действовать без единого признака.
    RAISE EXCEPTION
      'access_binding_subjects: выдача % не существует, область субъекта взять неоткуда',
      NEW.binding_id
      USING ERRCODE = 'foreign_key_violation',
            CONSTRAINT = 'access_binding_subjects_scope_fk';
  END IF;

  NEW.resource_type := rt;
  NEW.resource_id   := ri;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER access_binding_subjects_carries_scope_trg
  BEFORE INSERT ON kacho_iam.access_binding_subjects
  FOR EACH ROW
  EXECUTE FUNCTION kacho_iam.access_binding_subject_carries_scope();

-- Прежний однополосный ключ поглощён составным: `binding_id` — его ведущая
-- колонка, и каскад снятия сохранён.
ALTER TABLE kacho_iam.access_binding_subjects
  DROP CONSTRAINT access_binding_subjects_binding_id_fkey;

ALTER TABLE kacho_iam.access_binding_subjects
  ADD CONSTRAINT access_binding_subjects_scope_fk
  FOREIGN KEY (binding_id, resource_type, resource_id)
  REFERENCES kacho_iam.access_bindings (id, resource_type, resource_id)
  ON DELETE CASCADE ON UPDATE CASCADE;

-- Ради чего всё предыдущее: ОБА предиката вердикта сужают набор ДО чтения
-- строк, и ни один не остаётся фильтром после него.
--
-- Раскладка колонок — ИЛЛЮСТРАЦИЯ требуемого свойства, а не само требование:
-- любая другая, дающая то же свойство, ему удовлетворяет. Утверждается исход —
-- строк прочитано за вердикт, — а не порядок колонок и не форма плана.
CREATE INDEX access_binding_subjects_subject_scope_idx
  ON kacho_iam.access_binding_subjects (subject_type, subject_id, resource_type, resource_id);

-- Прочтение таблицы рёбер приводится к ОДНОМУ. Комментарий колонки в 0082
-- называет вторую сторону ребра «непосредственным предком», тогда как ключ
-- (object_type, object_id, depth) и проверка глубины 1..4 хранят ЗАМЫКАНИЕ —
-- строку на КАЖДОГО предка. Применённая миграция не правится (ban #5), поэтому
-- прочтение закрепляется здесь, в месте, которое читается запросом к базе, а не
-- глазами по файлу.
COMMENT ON TABLE kacho_iam.resource_parent_edge IS
  'Замыкание цепи предков объекта: строка на КАЖДОГО предка, глубина — расстояние (1 — непосредственный). Не список непосредственных рёбер: ключ уникален по (объект, глубина). Обход вверх есть ОДНО обращение с предикатом по объекту, а не рекурсия.';
COMMENT ON COLUMN kacho_iam.resource_parent_edge.parent_type IS
  'Тип предка на расстоянии depth. НЕ обязательно непосредственного: таблица хранит замыкание.';
COMMENT ON COLUMN kacho_iam.resource_parent_edge.parent_id IS
  'Идентификатор предка на расстоянии depth. НЕ обязательно непосредственного: таблица хранит замыкание.';

-- +goose Down

COMMENT ON COLUMN kacho_iam.resource_parent_edge.parent_id IS NULL;
COMMENT ON COLUMN kacho_iam.resource_parent_edge.parent_type IS NULL;
COMMENT ON TABLE kacho_iam.resource_parent_edge IS NULL;

DROP INDEX IF EXISTS kacho_iam.access_binding_subjects_subject_scope_idx;

ALTER TABLE kacho_iam.access_binding_subjects
  DROP CONSTRAINT IF EXISTS access_binding_subjects_scope_fk;

ALTER TABLE kacho_iam.access_binding_subjects
  ADD CONSTRAINT access_binding_subjects_binding_id_fkey
  FOREIGN KEY (binding_id) REFERENCES kacho_iam.access_bindings (id) ON DELETE CASCADE;

DROP TRIGGER IF EXISTS access_binding_subjects_carries_scope_trg ON kacho_iam.access_binding_subjects;
DROP FUNCTION IF EXISTS kacho_iam.access_binding_subject_carries_scope();

ALTER TABLE kacho_iam.access_binding_subjects
  DROP COLUMN IF EXISTS resource_id,
  DROP COLUMN IF EXISTS resource_type;

ALTER TABLE kacho_iam.access_bindings
  DROP CONSTRAINT IF EXISTS access_bindings_id_scope_uk;
