# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

variable "project_id" {
  description = "Проект-владелец всех машин набора."
  type        = string
}

variable "zone_id" {
  description = <<-EOT
    Зона ВСЕХ машин набора — одна переменная на набор.

    Зона машины и зона подсети каждого её интерфейса обязаны совпадать: край отвергает
    расхождение. Проверить это модулем нельзя — зону подсети знает только край, и выводить
    её разбором имени запрещено. Одна переменная хотя бы не даёт задать расхождение
    ВНУТРИ набора.
  EOT
  type        = string
}

variable "labels" {
  description = "Метки, накладываемые на все машины набора; метки машины дополняют их."
  type        = map(string)
  default     = {}
}

variable "instances" {
  description = <<-EOT
    Машины набора: ключ — имя машины в проекте.

    `boot_source` — откуда машина загружается: вид (`image`, `snapshot`, `volume`) и
    идентификатор. Источник ВНЕШНИЙ: том или образ этого же набора модуль не заводит, и
    ссылка на них шла бы через второй экземпляр модуля хранилища.

    `network_interfaces` — список интерфейсов; подсеть называется идентификатором, потому
    что подсети заводит модуль сети, а не этот.
  EOT
  type = map(object({
    machine_type_id         = string
    instance_kind           = optional(string, "VM")
    boot_source             = object({ type = string, id = string })
    description             = optional(string, "")
    labels                  = optional(map(string), {})
    hostname                = optional(string)
    service_account_id      = optional(string)
    cpu_guarantee_percent   = optional(number)
    assign_external_address = optional(bool, false)
    acknowledge_unreachable = optional(bool, false)
    network_interfaces = optional(list(object({
      subnet_id          = string
      security_group_ids = optional(list(string))
    })), [])
  }))
  default = {}

  validation {
    condition     = alltrue([for _, i in var.instances : contains(["image", "snapshot", "volume"], i.boot_source.type)])
    error_message = "boot_source.type принимает image, snapshot или volume."
  }

  validation {
    condition     = alltrue([for _, i in var.instances : trimspace(i.boot_source.id) != ""])
    error_message = "boot_source.id не может быть пустым: машине неоткуда загрузиться."
  }

  validation {
    condition     = alltrue([for _, i in var.instances : trimspace(i.machine_type_id) != ""])
    error_message = "machine_type_id обязателен: тип машины задаёт её вычислительные ресурсы."
  }

  validation {
    condition = alltrue([
      for _, i in var.instances : alltrue([for n in i.network_interfaces : trimspace(n.subnet_id) != ""])
    ])
    error_message = "У каждого сетевого интерфейса задаётся непустая подсеть."
  }

  # Достижимость выбирается ЯВНО, и это страж края, а не формальность модуля: без выбора
  # легко завести работающую машину, до которой снаружи не достучаться, и узнать об этом
  # лишь когда она понадобится. Модуль повторяет требование входом, чтобы отказ пришёл до
  # обращения к краю и назвал имя машины.
  validation {
    condition = alltrue([
      for _, i in var.instances :
      i.instance_kind != "VM" || i.assign_external_address || i.acknowledge_unreachable
    ])
    error_message = "Для машины вида VM задайте одно: assign_external_address = true (заказать внешний адрес) либо acknowledge_unreachable = true (согласиться, что снаружи до неё не достучаться)."
  }
}
