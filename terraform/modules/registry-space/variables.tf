# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

variable "project_id" {
  description = "Проект-владелец реестра."
  type        = string
}

variable "region_id" {
  description = "Регион реестра. Неизменяем: перенос между регионами краем не поддержан."
  type        = string
}

variable "name" {
  description = <<-EOT
    Имя реестра в пределах проекта.

    В путь загрузки образа оно НЕ попадает: путь строится по неизменяемому идентификатору
    (`$домен/$registryId/$репозиторий:$тег`). Имя можно менять, не ломая тех, кто тянет.
  EOT
  type        = string
}

variable "description" {
  description = "Произвольное описание реестра."
  type        = string
  default     = ""
}

variable "labels" {
  description = "Метки реестра и всех его репозиториев."
  type        = map(string)
  default     = {}
}

variable "default_repository_visibility" {
  description = <<-EOT
    Видимость новых репозиториев по умолчанию: `PRIVATE` или `PUBLIC`.

    Умолчание модуля — `PRIVATE`, и оно задано ЯВНО, а не оставлено краю: публичный
    репозиторий разрешает анонимную загрузку, и такое решение принимают, а не получают
    по невнимательности.
  EOT
  type        = string
  default     = "PRIVATE"

  validation {
    condition     = contains(["PRIVATE", "PUBLIC"], var.default_repository_visibility)
    error_message = "default_repository_visibility принимает PRIVATE или PUBLIC."
  }
}

variable "repositories" {
  description = <<-EOT
    Репозитории реестра: ключ — имя (может содержать косые черты, `team/service`),
    значение — описание и видимость.

    `visibility = null` означает «наследовать умолчание реестра».
  EOT
  type = map(object({
    description = optional(string, "")
    visibility  = optional(string)
  }))
  default = {}

  validation {
    condition = alltrue([
      for _, r in var.repositories :
      r.visibility == null || contains(["PRIVATE", "PUBLIC"], r.visibility)
    ])
    error_message = "visibility репозитория принимает PRIVATE, PUBLIC или null (наследовать)."
  }
}
