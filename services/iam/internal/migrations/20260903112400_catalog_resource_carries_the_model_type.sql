-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- catalog_resource_carries_the_model_type — СТРОКА КАТАЛОГА НЕСЁТ ИМЯ ТИПА
-- МОДЕЛИ ПРАВ, и потому тип, заведённый применением манифеста в РАБОТАЮЩЕМ
-- процессе, доезжает до проекции.
--
-- Задача продукта #1816, сценарий IAM-CT-2-14. Пункт 1 DoD эпика #1027.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧТО НЕВЕРНО СЕГОДНЯ
--
-- Читатели каталожного факта переведены на живые строки, и это закрыло ОДНО
-- направление — СНЯТИЕ (IAM-CT-2-06: снятый в работающем процессе тип пар не
-- даёт). Обратное направление — ЗАВЕДЕНИЕ — не закрыто ничем, и причина
-- структурная: имени типа модели прав строка НЕ НЕСЁТ. Его отдаёт словарь,
-- порождённый СБОРКОЙ, а тип, которого сборка не знала, получает от переходника
-- «не найдено», и читатель строку ПРОПУСКАЕТ.
--
-- Наблюдается это не отказом, а тишиной: строки каталога записаны, членство
-- модуля отвечает «да», роль создаётся без отказа — и проекция «роль → тип ×
-- глагол» пуста. Арендатор не получает ничего, и ни одна полоса об этом не
-- говорит.
--
-- Замер направления (`services/iam/internal/catalog`,
-- TestIAMCT2_14_AppliedTypeReachesTheProjection до этой миграции): пар
-- проекции по заведённому типу — 0 при живом членстве модуля и работающем
-- соседе.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ПОЧЕМУ КОЛОНКА, А НЕ ВЫВОД ИЗ ПАРЫ
--
-- Правило вывода `objectType ← <module>_<resource>` в этом дереве СНЯТО целиком
-- (`services/iam/internal/manifest/resources.go`): написание выбирает манифест
-- модуля и объявляет его дословно — у `storage` и `registry` имя ресурса
-- множественное, а тип единственного числа (`storage.volumes` →
-- `storage_volume`), у ярусных предков иерархии тип идёт БЕЗ приставки модуля
-- (`iam.account` → `account`). Вывести имя из пары нельзя ни одним выражением,
-- которое было бы верно на всех тридцати строках.
--
-- Значит имя обязано ПРИЕХАТЬ — той же строкой манифеста, которая объявляет
-- ресурс. Манифест его уже несёт (`resources[].objectType`, обязательное поле);
-- терялось оно в деривации строк каталога, а не в объявлении.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ТРЕТЬЕГО СЛОВАРЯ ЭТА КОЛОНКА НЕ ЗАВОДИТ
--
-- `authzmap/type_dictionaries.go` требует дословно: «второго переходника в
-- дереве не заводится». Здесь заводится не второй переходник, а МЕСТО ХРАНЕНИЯ
-- первого: словарь по-прежнему объявляет манифест модуля, а колонка — то, куда
-- он доезжает. Согласие колонки с порождённой сборкой таблицей на посеянных
-- строках держит страж старта (`seed.AssertCatalogParity`): он сверяет обе
-- стороны на КАЖДОМ старте и отказывает в пуске при расхождении.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ПОЧЕМУ ЗАПОЛНЕНИЕ ПЕРЕЧНЕМ, А НЕ ВЫРАЖЕНИЕМ
--
-- По той же причине, по какой заводится колонка: выражения, верного на всех
-- строках, не существует. Перечень ниже — те же тридцать пар, которыми каталог
-- посеян (27 живых плюс 3 снятых), и миграция ОТКАЗЫВАЕТСЯ завершиться, если
-- хоть одна строка осталась незаполненной: молчаливый пропуск дал бы ресурс,
-- по которому проекция пуста, — ровно тот дефект, который миграция закрывает.

-- +goose Up

-- Перепись ДО — чтобы самопроверка ниже утверждала «строк не потеряно» ЗАМЕРОМ,
-- а не выписанным числом. Выписанное устарело бы молча: мощность каталога уже
-- менялась дважды за две задачи.
CREATE TEMP TABLE _catalog_type_before ON COMMIT DROP AS
SELECT (SELECT count(*) FROM kacho_iam.catalog_resource)             AS resources_all,
       (SELECT count(*) FROM kacho_iam.catalog_resource WHERE live)  AS resources_live;

ALTER TABLE kacho_iam.catalog_resource
  ADD COLUMN object_type text;

-- Заполнение. Перечень покрывает ОБЕ половины каталога — живую и снятую:
-- у снятой строки имя типа историческое, и оно обязано быть, потому что колонка
-- становится NOT NULL. Строка без имени не отличалась бы от строки с пустым
-- именем, а пустое имя типа адресует отношение, которого нет ни в одной модели.
WITH d(dotted, object_type) AS (VALUES
    ('compute.guestAccessKey',            'compute_guest_access_key'),
    ('compute.instance',                  'compute_instance'),
    ('compute.placementGroup',            'compute_placement_group'),
    ('iam.accessBinding',                 'iam_access_binding'),
    ('iam.account',                       'account'),
    ('iam.group',                         'iam_group'),
    ('iam.project',                       'project'),
    ('iam.role',                          'iam_role'),
    ('iam.serviceAccount',                'iam_service_account'),
    ('iam.user',                          'iam_user'),
    ('loadbalancer.listeners',            'nlb_listener'),
    ('loadbalancer.networkLoadBalancers', 'nlb_network_load_balancer'),
    ('loadbalancer.targetGroups',         'nlb_target_group'),
    ('registry.registries',               'registry_registry'),
    ('registry.repositories',             'registry_repository'),
    ('storage.images',                    'storage_image'),
    ('storage.snapshots',                 'storage_snapshot'),
    ('storage.volumes',                   'storage_volume'),
    ('vpc.address',                       'vpc_address'),
    ('vpc.addressPool',                   'vpc_address_pool'),
    ('vpc.cidrGroup',                     'vpc_cidr_group'),
    ('vpc.gateway',                       'vpc_gateway'),
    ('vpc.network',                       'vpc_network'),
    ('vpc.networkInterface',              'vpc_network_interface'),
    ('vpc.routeTable',                    'vpc_route_table'),
    ('vpc.securityGroup',                 'vpc_security_group'),
    ('vpc.subnet',                        'vpc_subnet'),
    -- СНЯТЫЕ. Их типы ушли из канона вместе с расколом блочного хранения
    -- (`domain.retiredTypes`), но строки каталога не удаляются: по ним
    -- резолвится преемник, которым пользуется арендатор.
    ('compute.disk',                      'compute_disk'),
    ('compute.image',                     'compute_image'),
    ('compute.snapshot',                  'compute_snapshot')
)
UPDATE kacho_iam.catalog_resource cr
   SET object_type = d.object_type
  FROM d
 WHERE d.dotted = cr.dotted;

-- САМОПРОВЕРКА ЗАПОЛНЕНИЯ. Строка, оставшаяся без имени, означает, что перечень
-- чего-то не знал. Миграция отказывается завершиться и НАЗЫВАЕТ строку: отказ
-- дешевле молчаливого пропуска — пропущенная строка стала бы ресурсом, по
-- которому проекция пуста при действующей на вид роли.
-- +goose StatementBegin
DO $$
DECLARE leftover text;
BEGIN
  SELECT dotted INTO leftover
    FROM kacho_iam.catalog_resource
   WHERE object_type IS NULL
   LIMIT 1;
  IF leftover IS NOT NULL THEN
    RAISE EXCEPTION
      'catalog_resource: строка % осталась без имени типа модели прав. Проекция «роль → тип × глагол» по ней была бы ПУСТА при действующей на вид роли, и ни одна полоса об этом не сказала бы. Впишите пару в перечень этой миграции.', leftover;
  END IF;
END $$;
-- +goose StatementEnd

-- +kacho point-of-no-return: прежний образ пишет строку ресурса тремя колонками
-- (`INSERT INTO catalog_resource (module, resource, dotted)`) и об `object_type`
-- не знает. После `SET NOT NULL` такая вставка отвергается на ПЕРВОЙ же записи,
-- то есть откат ОБРАЗА поставки неисполним: секция Down на пути отката не
-- исполняется, схема остаётся новой, а старый применитель каталога перестаёт
-- заводить ресурсы. Откат — только вместе с этой миграцией, вручную.
--
-- Колонка ОБЯЗАНА быть NOT NULL, и это решение, а не строгость: строка без
-- имени типа модели прав ресурсом не является — по ней пуста проекция «роль →
-- тип × глагол», то есть ровно тот дефект, который миграция закрывает.
ALTER TABLE kacho_iam.catalog_resource
  ALTER COLUMN object_type SET NOT NULL;

-- Грамматика имени — та же, какой говорит канон модели прав: строчные латинские
-- буквы, цифры и подчёркивание, первый знак — буква. Проверка стоит здесь, а не
-- у читателя: инвариант внутри одной базы выражается конструкцией базы (ban #10).
ALTER TABLE kacho_iam.catalog_resource
  ADD CONSTRAINT catalog_resource_object_type_form
    CHECK (object_type ~ '^[a-z][a-z0-9_]*$');

-- Имя типа УНИКАЛЬНО среди живых строк. Два ресурса, отображённые в один тип
-- модели, дали бы проекции пару, по которой невозможно сказать, чей это ресурс:
-- обратное направление переходника (`DottedType`) стало бы неоднозначным, и
-- разрешалось бы оно порядком чтения. `live` входит в ключ по тому же образцу,
-- что у соседних `catalog_resource_live_uk` и `catalog_resource_dotted_live_uk`:
-- снятая строка слот живой не занимает.
ALTER TABLE kacho_iam.catalog_resource
  ADD CONSTRAINT catalog_resource_object_type_live_uk UNIQUE (object_type, live);

COMMENT ON COLUMN kacho_iam.catalog_resource.object_type IS
  'Имя типа МОДЕЛИ ПРАВ (vpc_network, account), которым адресуется отношение v_<глагол>. Объявляется строкой манифеста (resources[].objectType) и приезжает вместе с ресурсом: правила вывода из пары не существует — у storage/registry имя ресурса множественное, а тип единственного числа, у ярусных предков иерархии тип идёт без приставки модуля. Без этой колонки тип, заведённый применением манифеста в работающем процессе, не доезжал бы до проекции вовсе (#1816, IAM-CT-2-14).';

-- САМОПРОВЕРКА ИТОГА. Три величины сразу: строк не потеряно, имена уникальны
-- среди живых, и объём осмотренного назван. Одно число из трёх скрыло бы ровно
-- тот случай, ради которого проверка стоит.
-- +goose StatementBegin
DO $$
DECLARE
  before_all  bigint;
  before_live bigint;
  after_all   bigint;
  after_live  bigint;
  distinct_types bigint;
BEGIN
  SELECT resources_all, resources_live INTO before_all, before_live FROM _catalog_type_before;
  SELECT count(*) INTO after_all  FROM kacho_iam.catalog_resource;
  SELECT count(*) INTO after_live FROM kacho_iam.catalog_resource WHERE live;
  SELECT count(DISTINCT object_type) INTO distinct_types
    FROM kacho_iam.catalog_resource WHERE live;

  IF after_all <> before_all OR after_live <> before_live THEN
    RAISE EXCEPTION
      'catalog_resource: мощность каталога изменилась (было всего %, живых %; стало всего %, живых %). Эта миграция строк не заводит и не снимает.',
      before_all, before_live, after_all, after_live;
  END IF;

  IF distinct_types <> after_live THEN
    RAISE EXCEPTION
      'catalog_resource: живых строк %, различных имён типа % — два ресурса отображены в один тип модели, и обратное направление переходника стало бы неоднозначным.',
      after_live, distinct_types;
  END IF;

  RAISE NOTICE
    'имя типа модели в строке каталога: осмотрено строк % (живых %), различных имён типа %; ключ уникальности и грамматика заведены',
    after_all, after_live, distinct_types;
END;
$$;
-- +goose StatementEnd

-- +goose Down
--
-- Откат снимает КОЛОНКУ, а не строки: мощность каталога не меняется. После него
-- тип, заведённый применением манифеста в работающем процессе, снова перестаёт
-- доезжать до проекции — и это надо знать тому, кто откат применяет.
ALTER TABLE kacho_iam.catalog_resource
  DROP CONSTRAINT catalog_resource_object_type_live_uk;

ALTER TABLE kacho_iam.catalog_resource
  DROP CONSTRAINT catalog_resource_object_type_form;

ALTER TABLE kacho_iam.catalog_resource
  DROP COLUMN object_type;
