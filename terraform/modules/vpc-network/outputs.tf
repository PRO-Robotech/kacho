# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "network_id" {
  description = "Идентификатор сети. Неизменяем; по нему выполняется импорт."
  value       = kacho_vpc_network.this.id
}

output "default_security_group_id" {
  description = "Группа безопасности по умолчанию, созданная краем вместе с сетью."
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
