# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

variable "project_id" {
  description = "Проект, в котором заводятся машины."
  type        = string
}

variable "zone_id" {
  description = <<-EOT
    Зона размещения машин набора.

    Зона названа НА УРОВНЕ МОДУЛЯ, а не у каждой машины: подсеть интерфейса обязана
    быть в той же зоне, что и машина (когерентность размещения), поэтому набор,
    размазанный по зонам, потребовал бы по подсети на зону — и это уже другой
    модуль, а не параметр этого.
  EOT
  type        = string
}

variable "labels" {
  description = "Метки, проставляемые каждой машине набора."
  type        = map(string)
  default     = {}
}

variable "machines" {
  description = <<-EOT
    Машины набора. Ключ карты — суффикс имени, он же ключ выхода.

    `boot_source_type` перечислен явно, а не выведен по префиксу идентификатора:
    вывод по префиксу связал бы модуль с формой идентификатора чужого домена и
    сломался бы на его смене молча.
  EOT
  type = map(object({
    name               = string
    description        = optional(string)
    machine_type_id    = string
    boot_source_type   = string
    boot_source_id     = string
    subnet_id          = string
    security_group_ids = optional(list(string))
    placement_group_id = optional(string)
    service_account_id = optional(string)
  }))

  validation {
    condition = alltrue([
      for m in values(var.machines) :
      contains(["IMAGE", "SNAPSHOT", "VOLUME"], m.boot_source_type)
    ])
    error_message = "boot_source_type обязан быть IMAGE, SNAPSHOT либо VOLUME."
  }

  validation {
    condition     = alltrue([for m in values(var.machines) : m.boot_source_id != ""])
    error_message = "boot_source_id обязателен: машина не заводится без источника загрузки."
  }
}
