# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "network_id" {
  description = "Идентификатор сети. Неизменяем; по нему выполняется импорт."
  value       = kacho_vpc_network.this.id
}

output "default_security_group_id" {
  description = <<-EOT
    Группа безопасности по умолчанию — ПУСТАЯ СТРОКА, когда её не заводили.

    Здесь стояло «созданная краем вместе с сетью» без единой оговорки, тогда как заводится
    она только при `create_default_security_group = true`, а умолчание модуля — отказ.
    Пусто здесь означает «группы нет», а не «идентификатор пока неизвестен»: тот, кто
    подставит эту строку в ссылку не проверив, сошлётся в никуда.
  EOT
  value       = kacho_vpc_network.this.default_security_group_id
}

output "default_route_table_id" {
  description = "Таблица маршрутов по умолчанию, созданная краем вместе с сетью."
  value       = kacho_vpc_network.this.default_route_table_id
}

output "subnet_ids" {
  description = "Идентификаторы подсетей по их именам."
  value       = { for name, s in kacho_vpc_subnet.this : name => s.id }
}

output "subnets" {
  description = "Подсети целиком — идентификатор, размещение и таблица маршрутов."
  value = {
    for name, s in kacho_vpc_subnet.this : name => {
      id             = s.id
      placement_type = s.placement_type
      zone_id        = s.zone_id
      region_id      = s.region_id
      route_table_id = s.route_table_id
      ipv4_cidr      = s.ipv4_cidr_primary
    }
  }
}

output "gateway_id" {
  description = <<-EOT
    Идентификатор шлюза общего исхода; `null`, когда шлюз не заводили.

    `null` здесь — не «неизвестно», а «ресурса нет»: таблица маршрутизации, собирающаяся
    назвать шлюз следующим узлом, обязана этот случай различать, иначе она сошлётся на
    пустоту и получит отказ об отсутствии шлюза, которого никто не создавал.
  EOT
  value       = one(kacho_vpc_gateway.this[*].id)
}

output "network_interface_ids" {
  description = "Идентификаторы сетевых интерфейсов по их именам."
  value       = { for name, n in kacho_vpc_network_interface.this : name => n.id }
}

output "network_interfaces" {
  description = <<-EOT
    Интерфейсы целиком — идентификатор, подсеть, группы безопасности и статус.

    `status` выведен наружу намеренно: `AVAILABLE` означает «интерфейс создан, но ни к
    какой машине не подключён», а `ACTIVE` — «подключён и трафик идёт». Подключение
    выполняет владелец машины, вне этого модуля, и это единственное место, где итог его
    действия виден без обращения к краю.
  EOT
  value = {
    for name, n in kacho_vpc_network_interface.this : name => {
      id                 = n.id
      subnet_id          = n.subnet_id
      security_group_ids = n.security_group_ids
      status             = n.status
    }
  }
}

output "security_group_ids" {
  description = <<-EOT
    Идентификаторы групп безопасности по ключам карты `security_groups`.

    Выведены наружу не только для отчётности: правило одной группы вправе назвать целью
    другую группу, но ТОЛЬКО внешним идентификатором — ссылка группы на группу того же блока
    замкнула бы граф и была бы отвергнута циклом. Вторая цепочка групп заводится вторым
    экземпляром модуля, и вот идентификаторы, которые она получит на вход как
    `security_group_id` правила. Отсюда же их берут интерфейсы, заводимые вне модуля.
  EOT
  value       = { for name, g in kacho_vpc_security_group.this : name => g.id }
}

output "route_table_ids" {
  description = <<-EOT
    Идентификаторы таблиц маршрутизации по ключам карты `route_tables`.

    Подсетям ЭТОГО модуля они не нужны: там таблица называется ключом (`route_table`), и
    идентификатор модуль подставляет сам. Здесь они — для подсетей, заводимых вне модуля.
  EOT
  value       = { for name, t in kacho_vpc_route_table.this : name => t.id }
}

output "address_ids" {
  description = "Идентификаторы адресов по ключам карты `addresses`. Годятся как `v4_address_ids`/`v6_address_ids` интерфейса."
  value       = { for name, a in local.addresses : name => a.id }
}

output "addresses" {
  description = <<-EOT
    Адреса целиком — идентификатор, вид по данным края и сам выданный IP.

    `address` собирается из ВЫБРАННОЙ спецификации: сам адрес живёт внутри ветки, а какая
    ветка выбрана, знает только конфигурация. Без этой сборки вызывающему пришлось бы
    повторять свой же выбор вида, чтобы прочитать то, что край выдал.

    `type` и `ip_version` называет край — они и подтверждают, что выбранная ветка означала
    то, что предполагалось.
  EOT
  value = {
    for name, a in local.addresses : name => {
      id         = a.id
      type       = a.type
      ip_version = a.ip_version
      address = (
        a.external_ipv4 != null ? a.external_ipv4.address :
        a.internal_ipv4 != null ? a.internal_ipv4.address :
        a.internal_ipv6 != null ? a.internal_ipv6.address :
        a.external_ipv6 != null ? a.external_ipv6.address : null
      )
    }
  }
}
