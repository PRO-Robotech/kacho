# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

terraform {
  # 1.6 достаточно: межпеременных проверок здесь нет — все validation смотрят
  # только на собственную переменную. Поднимать требование до 1.9, как в
  # storage-set, значило бы просить у вызывающего версию, которой модуль не
  # пользуется.
  required_version = ">= 1.6"

  required_providers {
    kacho = {
      source = "PRO-Robotech/kacho"
    }
  }
}

# Каждый атрибут присваивается ОТДЕЛЬНО, а не переданным целиком объектом: у
# объекта переменной есть необязательные поля, и присваивание целиком отдало бы
# провайдеру null там, где вызывающий поле не называл, — то есть «снять значение»
# вместо «не трогать».
resource "kacho_compute_instance" "this" {
  for_each = var.machines

  project_id  = var.project_id
  name        = each.value.name
  description = each.value.description
  labels      = var.labels

  # Зона машины и зона подсети её интерфейса обязаны совпадать — это инвариант
  # платформы, а не соглашение модуля. Здесь он выполняется по построению: зона
  # одна на весь набор, и подсеть выбирает вызывающий под неё.
  zone_id         = var.zone_id
  machine_type_id = each.value.machine_type_id

  boot_source_type = each.value.boot_source_type
  boot_source_id   = each.value.boot_source_id

  subnet_id          = each.value.subnet_id
  security_group_ids = each.value.security_group_ids

  placement_group_id = each.value.placement_group_id
  service_account_id = each.value.service_account_id
}
