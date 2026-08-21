-- +goose Up
-- Строка субъекта появляется ВМЕСТЕ с выдачей — триггером, а не памятью автора.
--
-- ПРЕДМЕТ. Форма вердикта заходит в выдачи с пары «субъект + область» через
-- kacho_iam.access_binding_subjects. Выдача без дочерней строки невидима
-- вердикту ЦЕЛИКОМ: право записано, читается всеми списками — и не действует.
-- Отличить такое состояние от «права не выдавали» нельзя ничем, кроме этого
-- запроса, поэтому оно и прожило незамеченным.
--
-- ПОЧЕМУ НЕ ДИСЦИПЛИНОЙ ВЫЗЫВАЮЩЕГО. Выдача пишется одним оператором,
-- субъекты — другим, и второй зовёт лишь путь публичного создания выдачи.
-- Служебные пути, пишущие выдачу внутри собственной транзакции — выдача
-- администратора создателю проекта, приглашение пользователя с ролью, — зовут
-- только первый. Это ban #10 в чистом виде: инвариант, который держится
-- согласием двух операторов, нарушается молча на первом же новом пути.
--
-- ЗАМЕР на живом стенде 2026-08-21 (предикат — в теле обратного заполнения
-- ниже): выдач 450, дочерних строк 339, выдач без субъекта 111. Из них 110 —
-- административные на проект, то есть СОЗДАТЕЛИ проектов, и одна от
-- приглашения. Все 111 несли пару субъекта в самой выдаче, поэтому проекция
-- восстановима без потери данных.

-- +goose StatementBegin
-- CREATE, а не CREATE OR REPLACE: функция заведомо новая, и совпадение имени
-- означало бы, что её кто-то уже завёл — замена проглотила бы это молча.
CREATE FUNCTION kacho_iam.access_binding_projects_its_subject()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  -- Выдача без пары субъекта в самой строке спроецировать нечего. Такой строки
  -- не бывает (колонки NOT NULL), но условие оставлено явным: молчаливый выход
  -- лучше исключения на пути, где предмета нет.
  IF NEW.subject_type IS NULL OR NEW.subject_type = ''
     OR NEW.subject_id IS NULL OR NEW.subject_id = '' THEN
    RETURN NULL;
  END IF;

  -- Идемпотентно: путь, который субъектов пишет сам, получит свою строку
  -- первым либо этим оператором — но ровно одну. Дубль означал бы лишний
  -- проход по ветви выдач на КАЖДОМ вопросе о правах.
  INSERT INTO kacho_iam.access_binding_subjects
         (binding_id, subject_type, subject_id, ordinal, resource_type, resource_id)
  VALUES (NEW.id, NEW.subject_type, NEW.subject_id, 0, NEW.resource_type, NEW.resource_id)
  ON CONFLICT (binding_id, subject_type, subject_id) DO NOTHING;

  RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- AFTER, а не BEFORE: дочерняя строка ссылается на выдачу внешним ключом, и до
-- вставки родителя ссылаться не на что.
CREATE TRIGGER access_bindings_projects_its_subject_trg
  AFTER INSERT ON kacho_iam.access_bindings
  FOR EACH ROW
  EXECUTE FUNCTION kacho_iam.access_binding_projects_its_subject();

-- Обратное заполнение накопленного. Предикат тот же, каким дефект измерен:
--   SELECT count(*) FROM kacho_iam.access_bindings b
--    WHERE NOT EXISTS (SELECT 1 FROM kacho_iam.access_binding_subjects s
--                       WHERE s.binding_id = b.id);
-- После этой миграции он обязан давать ноль.
INSERT INTO kacho_iam.access_binding_subjects
       (binding_id, subject_type, subject_id, ordinal, resource_type, resource_id)
SELECT b.id, b.subject_type, b.subject_id, 0, b.resource_type, b.resource_id
  FROM kacho_iam.access_bindings b
 WHERE b.subject_type IS NOT NULL AND b.subject_type <> ''
   AND b.subject_id   IS NOT NULL AND b.subject_id   <> ''
   AND NOT EXISTS (SELECT 1 FROM kacho_iam.access_binding_subjects s
                    WHERE s.binding_id = b.id)
ON CONFLICT (binding_id, subject_type, subject_id) DO NOTHING;

COMMENT ON FUNCTION kacho_iam.access_binding_projects_its_subject() IS
  'Проецирует пару субъекта выдачи в access_binding_subjects. Без неё выдача, '
  'записанная служебным путём (создатель проекта, приглашение), невидима форме '
  'вердикта: право есть и не действует.';

-- +goose Down
DROP TRIGGER IF EXISTS access_bindings_projects_its_subject_trg ON kacho_iam.access_bindings;
DROP FUNCTION IF EXISTS kacho_iam.access_binding_projects_its_subject();
