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
  EOT
  type = map(object({
    zone_id           = optional(string)
    region_id         = optional(string)
    ipv4_cidr_primary = string
    description       = optional(string, "")
    labels            = optional(map(string), {})
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
}
