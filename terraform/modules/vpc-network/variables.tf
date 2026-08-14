# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

variable "project_id" {
  description = "Проект-владелец сети. Сменить его у существующей сети нельзя."
  type        = string
}

variable "name" {
  description = <<-EOT
    Имя сети в пределах проекта. Обязательно.

    Провайдер требует имя строже, чем край: это единственный способ найти уже созданный
    ресурс, если ответ на создание потерялся. Без имени повтор создал бы дубль.
  EOT
  type        = string
}

variable "description" {
  description = "Произвольное описание сети."
  type        = string
  default     = ""
}

variable "labels" {
  description = "Метки сети."
  type        = map(string)
  default     = {}
}
variable "ipv4_cidr_blocks" {
  description = <<-EOT
    Супернет сети — блоки, внутри которых обязаны лежать подсети.

    Список непуст by design: сеть без объявленного супернета подсеть НЕ примет, поэтому
    модуль, создающий сеть вместе с подсетями, обязан задать его сразу.
  EOT
  type        = list(string)

  validation {
    condition     = length(var.ipv4_cidr_blocks) > 0
    error_message = "Задайте хотя бы один блок: сеть без супернета не примет ни одной подсети."
  }
}

variable "subnets" {
  description = <<-EOT
    Подсети сети. Ключ карты — имя подсети в пределах проекта.

    У каждой подсети задаётся РОВНО ОДИН якорь размещения: зона либо регион. Региональная
    (anycast) подсеть зоны не несёт вовсе, и зональные проверки к ней не применяются.

    Таблица маршрутизации называется ОДНИМ из двух способов, и никогда обоими:

    - `route_table` — КЛЮЧ карты `route_tables` этого же модуля. Идентификатор модуль
      подставит сам, и получившаяся ссылка построит граф: подсеть снимется раньше таблицы.
      У несозданной таблицы идентификатора нет ни у кого, поэтому внутри модуля ссылаются
      ключом;
    - `route_table_id` — готовый идентификатор таблицы, заведённой ВНЕ модуля.

    Не задано ни одного — таблицу подставит край из `default_route_table_id` сети. Подсеть
    получает таблицу в момент СВОЕГО создания: заведение таблицы само по себе ни одну
    существующую подсеть не трогает.
  EOT
  type = map(object({
    zone_id           = optional(string)
    region_id         = optional(string)
    ipv4_cidr_primary = string
    description       = optional(string, "")
    labels            = optional(map(string), {})
    route_table       = optional(string)
    route_table_id    = optional(string)
  }))
  default = {}

  validation {
    condition = alltrue([
      for name, s in var.subnets :
      (s.zone_id != null) != (s.region_id != null)
    ])
    error_message = "У каждой подсети задаётся ровно одно из zone_id и region_id."
  }

  validation {
    condition = alltrue([
      for _, s in var.subnets :
      s.route_table == null || contains(keys(var.route_tables), s.route_table)
    ])
    error_message = format(
      "Подсети %s ссылаются на таблицу маршрутизации, которой нет в карте route_tables. Известные ключи: %s.",
      join(", ", [for k, s in var.subnets : k if s.route_table != null && !contains(keys(var.route_tables), s.route_table)]),
      length(var.route_tables) == 0 ? "карта route_tables пуста" : join(", ", keys(var.route_tables))
    )
  }

  validation {
    condition = alltrue([
      for _, s in var.subnets :
      s.route_table == null || s.route_table_id == null
    ])
    error_message = format(
      "У подсетей %s заданы и route_table, и route_table_id. Это два написания одного поля — ключ таблицы модуля и готовый идентификатор чужой, — и выбрать за вас, какое имелось в виду, модуль не вправе.",
      join(", ", [for k, s in var.subnets : k if s.route_table != null && s.route_table_id != null])
    )
  }
}

variable "create_gateway" {
  description = <<-EOT
    Заводить ли шлюз общего исхода.

    Умолчание — `false`, и это сохранение поведения: до сих пор модуль сети без шлюза
    создавал, и вызывающий, ничего не менявший, не должен получить новый ресурс от
    обновления модуля.

    Флаг заводит РЕСУРС, а не открывает исход: сам по себе шлюз трафик никуда не
    направляет — направляет таблица маршрутизации, называющая его следующим узлом. Читать
    его как выключатель доступа наружу нельзя.

    Шлюз принадлежит ПРОЕКТУ, а не сети: ссылки на сеть у ресурса нет вовсе. Поэтому два
    вызова этого модуля в одном проекте с включённым флагом заведут ДВА шлюза, а не
    поделят один.
  EOT
  type        = bool
  default     = false
}

variable "gateway_name" {
  description = <<-EOT
    Имя шлюза. `null` — вывести из имени сети с суффиксом `-gw`.

    Имя шлюза край принимает СТРОЖЕ имени сети: строчные латинские буквы, цифры и дефис,
    начинается буквой. Сеть же допускает и заглавные, и подчёркивание — поэтому выведенное
    имя годится не для всякой сети, и тогда его задают здесь явно.
  EOT
  type        = string
  default     = null

  validation {
    condition = var.gateway_name == null ? true : (
      can(regex("^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$", var.gateway_name))
    )
    error_message = "gateway_name: строчные латинские буквы, цифры и дефис; начинается буквой, заканчивается буквой или цифрой; до 63 символов."
  }
}

variable "gateway_description" {
  description = "Произвольное описание шлюза."
  type        = string
  default     = ""
}

variable "gateway_labels" {
  description = <<-EOT
    Метки шлюза.

    Отдельная переменная, а не переиспользование `labels`: та объявлена метками СЕТИ, и
    распространить её на новый ресурс значило бы сменить смысл переменной у тех, кто
    модуль уже вызывает, — молча и на применении, а не на чтении.
  EOT
  type        = map(string)
  default     = {}
}

variable "network_interfaces" {
  description = <<-EOT
    Сетевые интерфейсы. Ключ карты — имя интерфейса в пределах проекта.

    `subnet` — КЛЮЧ карты `subnets`, а не идентификатор подсети: идентификатор модуль
    подставит сам, и получившаяся ссылка построит граф — интерфейсы снимутся раньше
    подсети, которая их держит. Готовый идентификатор здесь не принимается именно
    поэтому: он разорвал бы ребро графа, и порядок сноса стал бы произвольным.

    Привязки к машине здесь нет и быть не может: край отвергает `instance_id` и `index`
    на создании интерфейса синхронно и с именем поля — подключение выполняет владелец
    машины, а не тот, кто заводит интерфейс.

    `security_group_ids` по умолчанию ПУСТ, и это значит «ни одной группы»: край на
    создании ничего не наследует. Группу сети по умолчанию модуль сюда не подставляет: при
    умолчании `create_default_security_group = false` её не существует вовсе, и
    подставилась бы пустая строка — ссылка в никуда. А там, где её заводят, она разрешает
    любой протокол отовсюду в обе стороны: привязка ТАКОЙ группы к интерфейсу — решение
    вызывающего, а не умолчание модуля.
  EOT
  type = map(object({
    subnet             = string
    description        = optional(string, "")
    labels             = optional(map(string), {})
    security_group_ids = optional(list(string), [])
    v4_address_ids     = optional(list(string), [])
    v6_address_ids     = optional(list(string), [])
  }))
  default = {}

  validation {
    condition = alltrue([
      for _, n in var.network_interfaces : contains(keys(var.subnets), n.subnet)
    ])
    error_message = "Поле subnet интерфейса — ключ карты subnets этого же модуля; такого ключа среди subnets нет."
  }

  validation {
    condition = alltrue([
      for _, n in var.network_interfaces :
      length(n.v4_address_ids) <= 1 && length(n.v6_address_ids) <= 1
    ])
    error_message = "У интерфейса не более одного адреса IPv4 и одного IPv6: несколько адресов набираются несколькими интерфейсами."
  }

  validation {
    condition = alltrue([
      for _, n in var.network_interfaces :
      alltrue([for id in concat(n.security_group_ids, n.v4_address_ids, n.v6_address_ids) : id != ""])
    ])
    error_message = "Пустая строка — не идентификатор: край ответит на неё отказом об отсутствии ресурса, которого вызывающий не называл."
  }
}

variable "cidr_groups" {
  description = <<-EOT
    Именованные наборы префиксов. Ключ карты — имя набора в пределах проекта.

    Набор существует ради ссылок на него: перечень сетей, повторённый в двадцати правилах,
    правится ОДИН раз здесь, а не в двадцати местах. Правила этого модуля называют набор
    ключом (`cidr_group` у правила) — идентификатор модуль подставит сам, и получившаяся
    ссылка построит граф: правило снимется раньше набора. Порядок несущий — край не удаляет
    набор, на который ссылается живое правило.

    Набор принадлежит ПРОЕКТУ, а не сети: ссылки на сеть в его контракте нет вовсе. Правило
    вправе назвать только набор СВОЕГО проекта.

    Семьи задаются РАЗНЫМИ полями: член чужого семейства край отвергает на входе, поэтому
    смешанный набор невыразим. Потолок — 64 члена на семейство; своей проверки этого здесь
    НЕТ намеренно: она была бы вторым, более слабым предикатом того же предмета, а
    расходятся такие пары молча и именно там, где расхождение не видно.
  EOT
  type = map(object({
    description    = optional(string, "")
    labels         = optional(map(string), {})
    v4_cidr_blocks = optional(list(string), [])
    v6_cidr_blocks = optional(list(string), [])
  }))
  default = {}
}

variable "security_groups" {
  description = <<-EOT
    Группы безопасности сети. Ключ карты — имя группы в пределах проекта.

    ГРУППА БЕЗ ПРАВИЛ НЕ ПРОПУСКАЕТ НИЧЕГО — это её смысл, а не пробел описания. Правила
    только РАЗРЕШАЮТ: запрещающего правила в контракте нет, приоритета и порядка у правил
    тоже нет, и что не разрешено явно — не проходит. Поэтому `rules = []` (и опущенное поле)
    означает интерфейс, закрытый в ОБЕ стороны, включая исходящий трафик: `EGRESS` тоже
    выдаётся правилом.

    Группа заводится в сети этого модуля, и сеть у неё НЕИЗМЕНЯЕМА: операции переноса группы
    между сетями у края не существует, смена сети пересоздаёт группу.

    У правила:

    - `direction` — `INGRESS` либо `EGRESS`. Своей копии этого перечня модуль не держит:
      имена сверяет провайдер по таблице контракта, а вторая копия разошлась бы с ней молча;
    - протокол — `protocol_name` ЛИБО `protocol_number`, не оба. Ни одного не задано — любой
      протокол. Номер `0` край не принимает (он неотличим от «протокол не задан»);
    - `ports` — обе границы вместе либо ничего: полудиапазона край не знает. Опущенный
      диапазон означает ЛЮБОЙ порт;
    - цель — РОВНО ОДНА из трёх: блоки адресов (`v4_cidr_blocks`/`v6_cidr_blocks`), другая
      группа (`security_group_id`), именованный набор префиксов (`cidr_group` — ключ карты
      `cidr_groups` этого модуля, либо `cidr_group_id` — готовый идентификатор набора,
      заведённого вне модуля; оба сразу задать нельзя).

    Набор — способ не копировать перечень сетей в каждое правило: правило называет НАБОР,
    и правка набора действует во всех правилах, которые на него сослались. Пустой набор
    целью быть не может — край отвергает такое правило, поэтому проверка входа ниже
    отвергает его раньше обращения к краю.

    `security_group_id` — ВНЕШНИЙ идентификатор, и ключом карты этого модуля он быть не может
    by construction: ссылка группы на группу того же блока — самоссылка, и конфигурация была
    бы отвергнута циклом ещё до края. Группа-цель обязана лежать в той же сети — это
    требование края, и оно на вызывающем.

    Правила задаются списком, а край хранит их НАБОРОМ: порядок не значит ничего, а два
    дословно одинаковых правила — одно правило (собственной личности, кроме своего значения,
    у правила нет). Список выбран ради адреса отказа: сообщение называет ключ группы и индекс
    в том списке, который написал вызывающий.
  EOT
  type = map(object({
    description = optional(string, "")
    labels      = optional(map(string), {})
    rules = optional(list(object({
      direction = string

      # Описание и метки ПРАВИЛА по умолчанию null, а не "" и не {} — в отличие от полей
      # самой группы выше: пустая строка и пустая карта означают для края то же, что
      # опускание, и обратно он отдаёт опускание. Два написания одного смысла разошлись бы
      # между настройкой и состоянием на первом же плане.
      description = optional(string)
      labels      = optional(map(string))

      protocol_name   = optional(string)
      protocol_number = optional(number)

      ports = optional(object({
        from_port = number
        to_port   = number
      }))

      v4_cidr_blocks    = optional(list(string))
      v6_cidr_blocks    = optional(list(string))
      security_group_id = optional(string)
      cidr_group        = optional(string)
      cidr_group_id     = optional(string)
    })), [])
  }))
  default = {}

  validation {
    condition = alltrue(flatten([
      for _, g in var.security_groups : [
        for r in g.rules :
        length(compact([
          r.v4_cidr_blocks == null && r.v6_cidr_blocks == null ? "" : "cidr_blocks",
          r.security_group_id == null ? "" : "security_group_id",
          r.cidr_group == null && r.cidr_group_id == null ? "" : "cidr_group",
        ])) == 1
      ]
    ]))
    error_message = format(
      "Правила %s задают не ровно одну цель. Цель — одна из трёх: блоки адресов, security_group_id либо именованный набор (cidr_group / cidr_group_id). Правило без цели край ОТВЕРГАЕТ синхронно, называя поле; проверка здесь ловит это раньше обращения к краю.",
      join(", ", flatten([
        for k, g in var.security_groups : [
          for i, r in g.rules : "${k}[${i}]"
          if length(compact([
            r.v4_cidr_blocks == null && r.v6_cidr_blocks == null ? "" : "cidr_blocks",
            r.security_group_id == null ? "" : "security_group_id",
            r.cidr_group == null && r.cidr_group_id == null ? "" : "cidr_group",
          ])) != 1
        ]
      ]))
    )
  }

  validation {
    condition = alltrue(flatten([
      for _, g in var.security_groups : [
        for r in g.rules : r.cidr_group == null || r.cidr_group_id == null
      ]
    ]))
    error_message = format(
      "У правил %s заданы и cidr_group, и cidr_group_id. Это два написания одной цели — ключ набора этого модуля и готовый идентификатор чужого, — и выбрать за вас, какое имелось в виду, модуль не вправе.",
      join(", ", flatten([
        for k, g in var.security_groups : [
          for i, r in g.rules : "${k}[${i}]"
          if r.cidr_group != null && r.cidr_group_id != null
        ]
      ]))
    )
  }

  validation {
    condition = alltrue(flatten([
      for _, g in var.security_groups : [
        for r in g.rules :
        r.cidr_group == null || contains(keys(var.cidr_groups), coalesce(r.cidr_group, "-"))
      ]
    ]))
    error_message = format(
      "Правила %s ссылаются на набор префиксов, которого нет в карте cidr_groups. Правило называет набор КЛЮЧОМ карты этого модуля: у несозданного набора идентификатора нет ни у кого. Известные ключи: %s.",
      join(", ", flatten([
        for k, g in var.security_groups : [
          for i, r in g.rules : "${k}[${i}]"
          if r.cidr_group != null && !contains(keys(var.cidr_groups), coalesce(r.cidr_group, "-"))
        ]
      ])),
      length(var.cidr_groups) == 0 ? "карта cidr_groups пуста" : join(", ", keys(var.cidr_groups))
    )
  }

  # Пустой набор целью быть НЕ МОЖЕТ: край отвергает такое правило («a rule cannot reference
  # an empty set»), потому что правило иначе либо потеряло бы фильтр, либо перестало
  # пропускать что-либо — оба исхода молчаливы. Проверка ограничена НАЗВАННЫМИ наборами:
  # объявить пустой набор, на который никто не ссылается, край позволяет, и запрещать это
  # модулю не за что.
  validation {
    condition = alltrue(flatten([
      for _, g in var.security_groups : [
        for r in g.rules :
        length(var.cidr_groups[r.cidr_group].v4_cidr_blocks) +
        length(var.cidr_groups[r.cidr_group].v6_cidr_blocks) > 0
        if r.cidr_group != null && contains(keys(var.cidr_groups), coalesce(r.cidr_group, "-"))
      ]
    ]))
    error_message = format(
      "Правила %s ссылаются на ПУСТОЙ набор префиксов. Край отвергает такое правило: набор без единого члена не сужает и не расширяет ничего, а молча меняет смысл правила. Задайте набору хотя бы один блок.",
      join(", ", flatten([
        for k, g in var.security_groups : [
          for i, r in g.rules : "${k}[${i}]"
          if r.cidr_group != null && contains(keys(var.cidr_groups), coalesce(r.cidr_group, "-")) &&
          length(var.cidr_groups[r.cidr_group].v4_cidr_blocks) +
          length(var.cidr_groups[r.cidr_group].v6_cidr_blocks) == 0
        ]
      ]))
    )
  }

  validation {
    condition = alltrue(flatten([
      for _, g in var.security_groups : [
        for r in g.rules : r.protocol_name == null || r.protocol_number == null
      ]
    ]))
    error_message = format(
      "У правил %s протокол задан дважды. В контракте это ОДНО поле с двумя написаниями, и отправить оба нельзя: пришлось бы выбрать за вас, какое из них вы имели в виду. Ни одного — законно и означает «любой протокол».",
      join(", ", flatten([
        for k, g in var.security_groups : [
          for i, r in g.rules : "${k}[${i}]"
          if r.protocol_name != null && r.protocol_number != null
        ]
      ]))
    )
  }

  validation {
    condition = alltrue(flatten([
      for _, g in var.security_groups : [
        for r in g.rules :
        (r.security_group_id == null || r.security_group_id != "")
      ]
    ]))
    error_message = format(
      "У правил %s цель задана пустой строкой. Пустая строка — не значение: край читает её как «цель не задана», а правило без цели он теперь ОТВЕРГАЕТ. Уберите поле или назовите цель.",
      join(", ", flatten([
        for k, g in var.security_groups : [
          for i, r in g.rules : "${k}[${i}]"
          if r.security_group_id == ""
        ]
      ]))
    )
  }
}

variable "route_tables" {
  description = <<-EOT
    Таблицы маршрутизации сети. Ключ карты — имя таблицы в пределах проекта.

    Заведение таблицы САМО ПО СЕБЕ не меняет ни одной подсети: подсеть получает свою таблицу
    в момент СВОЕГО создания. Чтобы таблица применялась, назовите её ключ в `route_table`
    нужных подсетей этого же модуля.

    Маршрут задаётся назначением (`destination_prefix`, запись CIDR с нулевыми разрядами
    узла) и следующим узлом (`next_hop_address`, IP-адрес).

    ВЕТКИ «СЛЕДУЮЩИЙ УЗЕЛ — ШЛЮЗ» НЕТ, и это не отставание модуля от контракта: край
    отвергает её синхронно («route the next hop by IP address»), а собственного адреса у
    шлюза не существует и в контракте. Значит шлюз этого модуля (`create_gateway`) назвать
    следующим узлом нечем — ни ключом, ни идентификатором, — и обходного пути тоже нет.
    Ключ для ссылки на шлюз здесь не заведён намеренно: он был бы ручкой, единственный исход
    которой — отказ.

    Маршруты задаются списком, и порядок сохраняется дословно: край адресует свои отказы
    индексом (`static_routes[2]…`), и у списка этот индекс — тот самый, что вы написали. Два
    маршрута с одним назначением через разные следующие узлы — законная запись (равноценные
    пути), и она сохраняется как написана.

    Проверок входа у этой переменной НЕТ намеренно. Годность записи CIDR и адреса проверяет
    провайдер тем же разбором, каким их читает край, и называет виновный индекс; проверка на
    HCL была бы ВТОРЫМ, более слабым предикатом того же предмета — а расходятся такие пары
    молча и именно там, где расхождение не видно.
  EOT
  type = map(object({
    description = optional(string, "")
    labels      = optional(map(string), {})
    routes = optional(list(object({
      destination_prefix = string
      next_hop_address   = string
      labels             = optional(map(string))
    })), [])
  }))
  default = {}
}

variable "addresses" {
  description = <<-EOT
    Адреса проекта. Ключ карты — имя адреса в пределах проекта.

    Адрес принадлежит ПРОЕКТУ, а не сети: поля сети в его контракте нет вовсе. С сетью этого
    модуля его связывает подсеть — внутренний адрес выдаётся из её блоков.

    Вид задаётся РОВНО ОДНОЙ спецификацией из четырёх — `external_ipv4`, `internal_ipv4`,
    `internal_ipv6`, `external_ipv6`, — и после создания НЕ МЕНЯЕТСЯ: в маске изменения края
    спецификации нет вовсе, поэтому правка вида, зоны, подсети или самого адреса пересоздаёт
    ресурс. Меняются только имя, описание, метки и защита от удаления.

    У внутреннего адреса подсеть называется КЛЮЧОМ карты `subnets` этого модуля, а не
    идентификатором: идентификатор присваивает край при создании, поэтому у подсети, которой
    ещё нет, его нет и у вызывающего. Ключ модуль разрешает сам — и тем строит граф: адрес
    снимется раньше подсети, из которой выдан.

    Адреса в ЧУЖОЙ подсети здесь нет намеренно: такой адрес к сети модуля не относится и
    заводится ресурсом рядом с модулем, где его подсеть видна вызывающему целиком.

    `address` в любой спецификации необязателен: не задан — край выдаст адрес сам (внутренний
    из блоков подсети, внешний из пула). Заданный внутренний обязан лежать в блоках своей
    подсети.

    Зона внешнего адреса необязательна, и ПУСТАЯ ЗОНА ЗАКОННА — это адрес из глобального
    пула, зоне не принадлежащий. Существование названной зоны модуль не проверяет: справочник
    зон ведёт край, а выводить его содержимое разбором строк запрещено — такая проверка молча
    проходила бы всегда, выглядя исполненной.

    `deletion_protection` — защита от удаления. Пока она включена, край отказывает в удалении
    адреса, и провайдер её НЕ снимает сам: поставьте `false`, примените, и лишь затем
    удаляйте. Отказ `destroy` при включённой защите — правильный отказ.
  EOT
  type = map(object({
    description         = optional(string, "")
    labels              = optional(map(string), {})
    deletion_protection = optional(bool)

    external_ipv4 = optional(object({
      address = optional(string)
      zone_id = optional(string)
    }))
    external_ipv6 = optional(object({
      address = optional(string)
      zone_id = optional(string)
    }))
    internal_ipv4 = optional(object({
      subnet  = string
      address = optional(string)
    }))
    internal_ipv6 = optional(object({
      subnet  = string
      address = optional(string)
    }))
  }))
  default = {}

  validation {
    condition = alltrue([
      for _, a in var.addresses :
      length(compact([
        a.external_ipv4 == null ? "" : "external_ipv4",
        a.internal_ipv4 == null ? "" : "internal_ipv4",
        a.internal_ipv6 == null ? "" : "internal_ipv6",
        a.external_ipv6 == null ? "" : "external_ipv6",
      ])) == 1
    ])
    error_message = format(
      "У адресов %s задано не ровно одно из external_ipv4, internal_ipv4, internal_ipv6, external_ipv6. Вид адреса — это выбор одной ветки, а не набор ручек: адрес без вида край не создаст, а двух видов у одного адреса не бывает.",
      join(", ", [
        for k, a in var.addresses : k
        if length(compact([
          a.external_ipv4 == null ? "" : "external_ipv4",
          a.internal_ipv4 == null ? "" : "internal_ipv4",
          a.internal_ipv6 == null ? "" : "internal_ipv6",
          a.external_ipv6 == null ? "" : "external_ipv6",
        ])) != 1
      ])
    )
  }

  validation {
    condition = alltrue(flatten([
      for _, a in var.addresses : [
        for s in compact([
          a.internal_ipv4 == null ? "" : a.internal_ipv4.subnet,
          a.internal_ipv6 == null ? "" : a.internal_ipv6.subnet,
        ]) : contains(keys(var.subnets), s)
      ]
    ]))
    error_message = format(
      "Адреса %s выдаются из подсети, которой нет в карте subnets. Внутренний адрес называет подсеть КЛЮЧОМ карты этого модуля: у несозданной подсети идентификатора нет ни у кого. Известные ключи: %s.",
      join(", ", [
        for k, a in var.addresses : k
        if length([
          for s in compact([
            a.internal_ipv4 == null ? "" : a.internal_ipv4.subnet,
            a.internal_ipv6 == null ? "" : a.internal_ipv6.subnet,
          ]) : s if !contains(keys(var.subnets), s)
        ]) > 0
      ]),
      length(var.subnets) == 0 ? "карта subnets пуста" : join(", ", keys(var.subnets))
    )
  }
}
