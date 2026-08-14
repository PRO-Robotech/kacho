# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "instance_ids" {
  description = "Идентификаторы машин по ключам карты `machines`."
  value       = { for k, m in kacho_compute_instance.this : k => m.id }
}

output "instance_fqdns" {
  description = <<-EOT
    Полные доменные имена машин по ключам карты `machines`.

    Выведены наружу потому, что это единственная координата машины, пригодная для
    ссылки из соседнего модуля: адрес интерфейса принадлежит подсети и меняется
    при пересоздании, а идентификатор не разрешается в имя ничем, кроме запроса к
    краю.
  EOT
  value       = { for k, m in kacho_compute_instance.this : k => m.fqdn }
}

output "instance_statuses" {
  description = <<-EOT
    Состояния машин по ключам карты `machines`.

    Состояние выводится наружу намеренно: мутации платформы асинхронны, и вызывающий,
    строящий поверх набора что-то ещё, обязан иметь возможность увидеть, что машина
    отвечает не «создана», а «создаётся». Умалчивать об этом значило бы предлагать
    считать применённую настройку работающей машиной.
  EOT
  value       = { for k, m in kacho_compute_instance.this : k => m.status }
}

output "guest_access_key_ids" {
  description = "Идентификаторы заведённых ключей входа по ключу карты."
  value       = { for k, v in kacho_compute_guest_access_key.this : k => v.id }
}

# Отпечатки — единственное, чем арендатор сверяет, ТОТ ЛИ ключ доехал: материал
# он и так знает, а отпечаток считает край.
output "guest_access_key_fingerprints" {
  description = "Отпечатки заведённых ключей входа по ключу карты."
  value       = { for k, v in kacho_compute_guest_access_key.this : k => v.fingerprint }
}

output "placement_group_ids" {
  description = "Идентификаторы заведённых групп размещения по ключу карты."
  value       = { for k, v in kacho_compute_placement_group.this : k => v.id }
}
