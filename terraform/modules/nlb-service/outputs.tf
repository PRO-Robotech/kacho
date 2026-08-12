# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "load_balancer_id" {
  description = "Идентификатор балансировщика."
  value       = kacho_nlb_load_balancer.this.id
}

output "target_group_id" {
  description = "Идентификатор группы целей."
  value       = kacho_nlb_target_group.this.id
}

output "v4_address_id" {
  description = <<-EOT
    Выделенный адрес IPv4 — идентификатор ресурса адреса, а не сама строка адреса.

    Адресация в Kachō идёт по неизменяемому идентификатору; сам адрес читается у ресурса
    адреса в vpc.
  EOT
  value       = kacho_nlb_load_balancer.this.v4_address_id
}

output "v6_address_id" {
  description = "Выделенный адрес IPv6 — идентификатор ресурса адреса."
  value       = kacho_nlb_load_balancer.this.v6_address_id
}

output "listener_ids" {
  description = "Идентификаторы слушателей по ключам входной карты."
  value       = { for k, l in kacho_nlb_listener.this : k => l.id }
}

output "listener_substatus" {
  description = <<-EOT
    Второй статус каждого слушателя: `OK` или `MISCONFIGURED`.

    Выведен наружу намеренно: `MISCONFIGURED` означает, что слушатель создан, но раздавать
    ему некуда, — и это единственное место, где такое видно без обращения к краю.
  EOT
  value       = { for k, l in kacho_nlb_listener.this : k => l.substatus }
}
