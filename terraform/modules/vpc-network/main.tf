# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

terraform {
  # 1.9, а не 1.6: проверки входа этого модуля ссылаются на ДРУГУЮ переменную — ключ таблицы
  # маршрутизации у подсети и ключ подсети у внутреннего адреса обязаны быть ключами
  # соответствующих карт. Межпеременные validation появились в 1.9; на 1.6 такая проверка не
  # разбирается вовсе.
  #
  # Ограничение занижено было и ДО этой правки: проверка «поле subnet интерфейса — ключ карты
  # subnets» межпеременная ровно так же, а объявление обещало 1.6. Обещание, которого модуль
  # не выполнял, здесь приведено к тому, что он делает, — а не расширено вместе с новыми
  # проверками.
  #
  # Альтернативой была бы precondition у ресурса, но она проигрывает гонку: обращение
  # `kacho_vpc_route_table.this[<чужой ключ>]` в теле ресурса даёт «Invalid index» — отказ, не
  # называющий ни карты, ни известных ключей.
  required_version = ">= 1.9"

  required_providers {
    kacho = {
      source = "PRO-Robotech/kacho"
    }
  }
}

locals {
  # Имя шлюза выводится из имени сети, чтобы флаг включался одной строкой и не тянул за
  # собой обязательную переменную. Явное `gateway_name` сильнее вывода.
  #
  # Тернарник, а не coalesce: coalesce пропускает и пустую строку, то есть подменял бы
  # заведомо неверный ввод выведенным именем — отказ превратился бы в тихую подстановку.
  gateway_name = var.gateway_name == null ? "${var.name}-gw" : var.gateway_name
}

resource "kacho_vpc_network" "this" {
  project_id       = var.project_id
  name             = var.name
  description      = var.description
  labels           = var.labels
  ipv4_cidr_blocks = var.ipv4_cidr_blocks

  # Решение о группе безопасности по умолчанию принимается ЗДЕСЬ, а не краем. Поле
  # необязательно, и незаданным край разрешает его настройкой своего РАЗВЁРТЫВАНИЯ
  # (`KACHO_VPC_DEFAULT_SG_INLINE`) — то есть одна и та же конфигурация давала бы разный
  # итог на разных стендах, и разный молча. Группа заводится с правилами «любой протокол
  # отовсюду» в обе стороны, поэтому умолчание такого рода не оставляют краю.
}

# Группа безопасности заводится В СЕТИ модуля и ссылается на неё ПО ИДЕНТИФИКАТОРУ: ссылка
# строит граф, и снос идёт в обратном порядке — группы раньше сети. Иначе она здесь и не
# задавалась бы: сеть у группы обязательна и неизменяема, операции переноса группы между
# сетями у края не существует.
#
# ГРУППА БЕЗ ПРАВИЛ НЕ ПРОПУСКАЕТ НИЧЕГО, и это её смысл, а не пробел описания: правила
# только РАЗРЕШАЮТ, запрещающего правила в контракте нет, приоритета и порядка у правил нет.
# Пустой список закрывает интерфейс в обе стороны, включая исходящий трафик.
#
# Целью правила НЕ может быть группа этого же модуля по ключу: обращение к
# `kacho_vpc_security_group.this[…]` из тела того же блока — самоссылка, и конфигурация была
# бы отвергнута циклом ещё до края. Поэтому `security_group_id` правила принимает ВНЕШНИЙ
# идентификатор; край требует, чтобы эта группа лежала в той же сети.
resource "kacho_vpc_security_group" "this" {
  for_each = var.security_groups

  project_id  = var.project_id
  network_id  = kacho_vpc_network.this.id
  name        = each.key
  description = each.value.description
  labels      = each.value.labels

  # Правило собирается ПОЛЕ ЗА ПОЛЕМ: поле, добавленное во входную запись, не уедет к краю,
  # пока его не назвали в этой строке. Плюс тип атрибута схемы задаётся провайдером и
  # совпадать с типом переменной модуля не обязан — передача записи целиком дала бы «Value
  # Conversion Error», отказ, из которого не видно ни поля, ни причины.
  rules = [
    for rule in each.value.rules : {
      direction       = rule.direction
      description     = rule.description
      labels          = rule.labels
      protocol_name   = rule.protocol_name
      protocol_number = rule.protocol_number

      # Диапазон портов собирается ТОЛЬКО когда задан: опущенный `ports` означает у края
      # «любой порт», и это единственный способ так сказать. Границы лежат в одном объекте,
      # а не двумя отдельными числами, потому что полудиапазона край не знает — так «задана
      # только нижняя граница» не выражается вовсе.
      ports = rule.ports == null ? null : {
        from_port = rule.ports.from_port
        to_port   = rule.ports.to_port
      }

      # Блоки собираются в цель, только если задано хотя бы одно семейство: `cidr_blocks`
      # без единого блока край принимает МОЛЧА и отдаёт правило без цели — то есть как
      # другое правило, чем написал вызывающий. Сами списки уезжают как написаны: подменять
      # пустой список отсутствием значило бы принять поле и не применить его.
      cidr_blocks = rule.v4_cidr_blocks == null && rule.v6_cidr_blocks == null ? null : {
        v4_cidr_blocks = rule.v4_cidr_blocks
        v6_cidr_blocks = rule.v6_cidr_blocks
      }

      security_group_id = rule.security_group_id
    }
  ]
}

# Таблица маршрутизации тоже заводится В СЕТИ и ссылается на неё по идентификатору — снос в
# обратном порядке, таблицы раньше сети.
#
# Заведение таблицы САМО ПО СЕБЕ не меняет ни одной подсети: подсеть получает свою таблицу в
# момент СВОЕГО создания. Чтобы таблица применялась, её называют в `route_table` подсети —
# для этого ключ карты и разрешается ниже.
#
# Следующий узел маршрута — ТОЛЬКО адрес. Ветки «через шлюз» у края нет: он отвергает её
# синхронно, и собственного адреса у `kacho_vpc_gateway` не существует даже в контракте, —
# поэтому шлюз этого модуля назвать следующим узлом нечем, и обходного пути тоже нет. Ключ
# для ссылки на шлюз здесь не заводится: он был бы ручкой, единственный исход которой отказ.
resource "kacho_vpc_route_table" "this" {
  for_each = var.route_tables

  project_id  = var.project_id
  network_id  = kacho_vpc_network.this.id
  name        = each.key
  description = each.value.description
  labels      = each.value.labels

  # Маршруты — СПИСОК, и порядок сохраняется дословно: край адресует свои отказы индексом
  # (`static_routes[2]…`), и у списка этот индекс совпадает с индексом в конфигурации.
  static_routes = [
    for route in each.value.routes : {
      destination_prefix = route.destination_prefix
      next_hop_address   = route.next_hop_address
      labels             = route.labels
    }
  ]
}

# Подсети ссылаются на сеть по идентификатору, а не по имени: ссылка строит граф
# зависимостей, и уничтожение пойдёт в обратном порядке — подсети раньше сети. Полагаться
# на «неверный порядок даст ошибку» здесь нельзя: часть связей при неверном порядке не
# падает, а молча обнуляется.
resource "kacho_vpc_subnet" "this" {
  for_each = var.subnets

  project_id        = var.project_id
  network_id        = kacho_vpc_network.this.id
  name              = each.key
  description       = each.value.description
  labels            = each.value.labels
  zone_id           = each.value.zone_id
  region_id         = each.value.region_id
  ipv4_cidr_primary = each.value.ipv4_cidr_primary

  # Ключ карты `route_tables` разрешается ЗДЕСЬ в идентификатор — и тем строит ребро графа:
  # подсеть снимается раньше таблицы, на которую указывает. Готовый `route_table_id` остаётся
  # для таблиц, заведённых вне модуля; у несозданной таблицы идентификатора нет, поэтому
  # внутри модуля ссылаются ключом. Оба написания сразу отвергает проверка входа: выбирать за
  # вызывающего, какое из них он имел в виду, модуль не вправе.
  route_table_id = (
    each.value.route_table == null
    ? each.value.route_table_id
    : kacho_vpc_route_table.this[each.value.route_table].id
  )
}

# Шлюз общего исхода. Собственных настроек у него нет: контракт объявляет спецификацию
# пустой, и шлюз здесь — сам факт наличия исхода, а не набор ручек.
#
# Он принадлежит ПРОЕКТУ, а не сети: ссылки на сеть у ресурса нет, поэтому ребра графа
# между ним и сетью не возникает — снос идёт независимо. Дописать `depends_on` ради
# видимости порядка нельзя: это объявило бы гарантию, которой край не даёт.
resource "kacho_vpc_gateway" "this" {
  count = var.create_gateway ? 1 : 0

  project_id  = var.project_id
  name        = local.gateway_name
  description = var.gateway_description
  labels      = var.gateway_labels

  lifecycle {
    # Проверка стоит здесь, а не в переменной: она смотрит на ВЫВЕДЕННОЕ имя, то есть на
    # сочетание двух переменных, и обязана молчать, пока шлюз не заводят, — имя сети с
    # заглавной буквой законно ровно до того момента, как из него выводят имя шлюза.
    precondition {
      condition     = can(regex("^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$", local.gateway_name))
      error_message = "Имя шлюза «${local.gateway_name}», выведенное из имени сети, край не примет: у шлюза разрешены строчные латинские буквы, цифры и дефис. Задайте gateway_name явно."
    }
  }
}

# Интерфейс ссылается на подсеть ПО ИДЕНТИФИКАТОРУ, взятому по ключу карты подсетей: ссылка
# строит граф, и снос идёт в обратном порядке — интерфейсы раньше подсети. Порядок несущий:
# край не удаляет подсеть, из адресов которой кто-то выделен.
#
# `instance_id` и `index` модуль не выставляет и не принимает: край отвергает их на этом
# пути синхронно и с именем поля — подключение к машине выполняет её владелец, потому что
# только он может проверить инварианты привязки (та же зона, принадлежность машины, номер
# слота). Принять поле и не исполнить обещание — не исход.
resource "kacho_vpc_network_interface" "this" {
  for_each = var.network_interfaces

  project_id  = var.project_id
  name        = each.key
  description = each.value.description
  labels      = each.value.labels
  subnet_id   = kacho_vpc_subnet.this[each.value.subnet].id

  # Запись карты разбирается ПОЛЕ ЗА ПОЛЕМ, и каждое названо здесь явно.
  #
  # Здесь стояло объяснение через «Value Conversion Error» на объектном атрибуте, чей тип
  # включает вычисляемые поля (`status`, `created_at`). Оно списано с модуля, где такой
  # атрибут есть, и к этому ресурсу не относится: у интерфейса объектных атрибутов нет
  # вовсе — `status` и `created_at` лежат СОСЕДЯМИ этих полей, а не внутри них, и передать
  # сюда запись целиком нечему. Причина разбора по полям здесь другая и проверяемая: поле,
  # добавленное во входную запись, не уедет к краю, пока его не назвали в этой строке.
  security_group_ids = each.value.security_group_ids
  v4_address_ids     = each.value.v4_address_ids
  v6_address_ids     = each.value.v6_address_ids
}

# Адрес принадлежит ПРОЕКТУ, а не сети: поля сети в его контракте нет вовсе. С сетью этого
# модуля его связывает подсеть — внутренний адрес выдаётся из её блоков, и подсеть названа
# КЛЮЧОМ карты `subnets`. Ключ разрешается здесь в идентификатор, и получившаяся ссылка
# строит ребро графа: адреса снимаются раньше подсети, из которой выданы. Порядок несущий —
# край не удаляет подсеть, из адресов которой кто-то выделен.
#
# Вид адреса задаётся РОВНО ОДНОЙ спецификацией из четырёх и после создания не меняется: в
# маске изменения края спецификации нет вовсе, поэтому правка вида, зоны, подсети или самого
# адреса ПЕРЕСОЗДАЁТ ресурс. Провайдер говорит это заменой, а не видом применённой правки.
#
# ЧЕТЫРЕ БЛОКА, А НЕ ОДИН с `for_each` по всей карте, — и это не стиль, а единственная рабочая
# форма. Проверку настройки провайдер выполняет ОДИН раз на блок, ещё до того как разложить
# его по экземплярам, и `each.value` в этот момент НЕИЗВЕСТНО. Неизвестную спецификацию
# провайдер намеренно засчитывает за ЗАДАННУЮ (иначе он отвергал бы законную ссылку на ещё не
# созданную подсеть), поэтому у одного блока с четырьмя условными спецификациями все четыре
# читаются как заданные — и «ровно один вид» не выполняется НИКОГДА, даже при пустой карте.
# Проверено прогоном: одна такая попытка роняла весь набор проб ещё на первой из них.
#
# Разложенные по виду блоки называют свою спецификацию БЕЗУСЛОВНО, а прочие три не называют
# вовсе — счёт заданных равен единице при любом состоянии `each.value`. Ключи карт не
# пересекаются by construction: вид у адреса ровно один, и это держит проверка входа.
locals {
  # Отбор по виду. Выражен один раз здесь, а не четырежды в блоках: разойдись условие отбора
  # с условием сборки — адрес попал бы в блок, чью спецификацию собрать не из чего.
  addresses_external_ipv4 = { for name, a in var.addresses : name => a if a.external_ipv4 != null }
  addresses_external_ipv6 = { for name, a in var.addresses : name => a if a.external_ipv6 != null }
  addresses_internal_ipv4 = { for name, a in var.addresses : name => a if a.internal_ipv4 != null }
  addresses_internal_ipv6 = { for name, a in var.addresses : name => a if a.internal_ipv6 != null }

  # Все адреса модуля одной картой — для выходов. Ключи не пересекаются, поэтому merge ничего
  # не затирает: у адреса ровно один вид, и это проверено на входе.
  addresses = merge(
    kacho_vpc_address.external_ipv4,
    kacho_vpc_address.external_ipv6,
    kacho_vpc_address.internal_ipv4,
    kacho_vpc_address.internal_ipv6,
  )
}

resource "kacho_vpc_address" "external_ipv4" {
  for_each = local.addresses_external_ipv4

  project_id          = var.project_id
  name                = each.key
  description         = each.value.description
  labels              = each.value.labels
  deletion_protection = each.value.deletion_protection

  # Спецификация собирается ПОЛЕ ЗА ПОЛЕМ. Пустая зона законна и означает адрес из
  # глобального пула, зоне не принадлежащий, — в отличие от подсети, где якорь размещения
  # обязателен.
  external_ipv4 = {
    address = each.value.external_ipv4.address
    zone_id = each.value.external_ipv4.zone_id
  }
}

resource "kacho_vpc_address" "external_ipv6" {
  for_each = local.addresses_external_ipv6

  project_id          = var.project_id
  name                = each.key
  description         = each.value.description
  labels              = each.value.labels
  deletion_protection = each.value.deletion_protection

  external_ipv6 = {
    address = each.value.external_ipv6.address
    zone_id = each.value.external_ipv6.zone_id
  }
}

resource "kacho_vpc_address" "internal_ipv4" {
  for_each = local.addresses_internal_ipv4

  project_id          = var.project_id
  name                = each.key
  description         = each.value.description
  labels              = each.value.labels
  deletion_protection = each.value.deletion_protection

  # Ключ карты `subnets` разрешается в идентификатор ЗДЕСЬ — тем и строится ребро графа.
  # Зону адрес не несёт: он наследует её от подсети.
  internal_ipv4 = {
    address   = each.value.internal_ipv4.address
    subnet_id = kacho_vpc_subnet.this[each.value.internal_ipv4.subnet].id
  }
}

resource "kacho_vpc_address" "internal_ipv6" {
  for_each = local.addresses_internal_ipv6

  project_id          = var.project_id
  name                = each.key
  description         = each.value.description
  labels              = each.value.labels
  deletion_protection = each.value.deletion_protection

  internal_ipv6 = {
    address   = each.value.internal_ipv6.address
    subnet_id = kacho_vpc_subnet.this[each.value.internal_ipv6.subnet].id
  }
}
