# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

terraform {
  required_version = ">= 1.9"
  required_providers {
    kacho = {
      source = "PRO-Robotech/kacho"
    }
  }
}

# Значения собираются ПОЛЕ ЗА ПОЛЕМ, а не передачей переменной целиком: тип переменной
# модуля фиксирован её объявлением, а тип атрибута схемы включает вычисляемые поля,
# которых во входной переменной быть не должно. Передача целиком даёт «Value Conversion
# Error» — отказ, чей текст не называет ни поля, ни причины.
resource "kacho_compute_instance" "this" {
  for_each = var.instances

  project_id  = var.project_id
  zone_id     = var.zone_id
  name        = each.key
  description = each.value.description
  labels      = merge(var.labels, each.value.labels)

  instance_kind         = each.value.instance_kind
  machine_type_id       = each.value.machine_type_id
  hostname              = each.value.hostname
  service_account_id    = each.value.service_account_id
  cpu_guarantee_percent = each.value.cpu_guarantee_percent

  assign_external_address = each.value.assign_external_address
  acknowledge_unreachable = each.value.acknowledge_unreachable

  boot_source = {
    type = each.value.boot_source.type
    id   = each.value.boot_source.id
  }

  network_interface_specs = [
    for n in each.value.network_interfaces : {
      subnet_id          = n.subnet_id
      security_group_ids = n.security_group_ids
    }
  ]
}
