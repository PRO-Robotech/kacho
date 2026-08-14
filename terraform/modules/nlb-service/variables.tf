# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

variable "project_id" {
  description = "Проект-владелец. Сменить его у существующего балансировщика нельзя."
  type        = string
}

variable "region_id" {
  description = <<-EOT
    Регион балансировщика И группы целей.

    Одна переменная на оба ресурса намеренно: край требует их совпадения, и две
    переменные позволили бы задать расхождение, которое отвергнется только на apply.
  EOT
  type        = string
}

variable "name" {
  description = <<-EOT
    Основа имён. Балансировщик получает его как есть, группа целей — с суффиксом `-tg`,
    слушатели — с ключом своей записи в `listeners`.

    Имя обязательно: провайдер находит по нему уже созданный ресурс, если ответ на
    создание потерялся.
  EOT
  type        = string
}

variable "placement" {
  description = <<-EOT
    Посадка балансировщика: `EXTERNAL_REGIONAL`, `INTERNAL_REGIONAL` или `INTERNAL_ZONAL`.

    Она задаёт и обращённость наружу, и зональность сразу — отдельных переключателей нет.
    Посадка обязана быть согласована с подсетью источника адреса: зональный балансировщик
    берёт адрес из зональной подсети, региональный — из региональной.
  EOT
  type        = string

  validation {
    condition     = contains(["EXTERNAL_REGIONAL", "INTERNAL_REGIONAL", "INTERNAL_ZONAL"], var.placement)
    error_message = "placement принимает EXTERNAL_REGIONAL, INTERNAL_REGIONAL или INTERNAL_ZONAL."
  }
}

variable "v4_source" {
  description = <<-EOT
    Откуда взять адрес IPv4: ровно одно из `subnet_id` (край выделит адрес сам) или
    `address_id` (готовый адрес). `null` — балансировщик без адреса IPv4.
  EOT
  type = object({
    subnet_id  = optional(string)
    address_id = optional(string)
  })
  default = null

  validation {
    condition = var.v4_source == null ? true : (
      (var.v4_source.subnet_id == null) != (var.v4_source.address_id == null)
    )
    error_message = "В v4_source задаётся ровно одно из subnet_id и address_id."
  }
}

variable "v6_source" {
  description = "Откуда взять адрес IPv6. Та же форма, что у v4_source."
  type = object({
    subnet_id  = optional(string)
    address_id = optional(string)
  })
  default = null

  validation {
    condition = var.v6_source == null ? true : (
      (var.v6_source.subnet_id == null) != (var.v6_source.address_id == null)
    )
    error_message = "В v6_source задаётся ровно одно из subnet_id и address_id."
  }
}

variable "backend_port" {
  description = "Порт, на который группа целей раздаёт трафик."
  type        = number
}

variable "health_check" {
  description = <<-EOT
    Проверка живости. Ровно одна проба из четырёх — `tcp`, `http`, `https`, `grpc`.

    Умолчание — проба соединением на порт группы: она ничего не требует от приложения и
    потому годится как отправная точка. Пороги и сроки заданы явно, а не оставлены краю:
    край их не подставляет и отвергает запрос без них.
  EOT
  type = object({
    interval            = optional(string, "5s")
    timeout             = optional(string, "2s")
    healthy_threshold   = optional(number, 2)
    unhealthy_threshold = optional(number, 2)
    tcp                 = optional(object({ port = optional(number) }))
    http = optional(object({
      port           = optional(number)
      path           = optional(string)
      expected_codes = optional(string)
      host           = optional(string)
      headers        = optional(map(string))
    }))
    https = optional(object({
      port           = optional(number)
      path           = optional(string)
      expected_codes = optional(string)
      host           = optional(string)
      headers        = optional(map(string))
    }))
    grpc = optional(object({
      port         = optional(number)
      service_name = optional(string)
    }))
  })
  default = { tcp = {} }

  validation {
    condition = length([
      for p in [var.health_check.tcp, var.health_check.http, var.health_check.https, var.health_check.grpc] :
      p if p != null
    ]) == 1
    error_message = "В health_check задаётся ровно одна проба: tcp, http, https или grpc."
  }
}

variable "targets" {
  description = <<-EOT
    Цели группы. У каждой ровно одна форма адресации.

    Это НАБОР по смыслу: порядок краем не сохраняется и ни на что не влияет.
  EOT
  type = list(object({
    instance_id = optional(string)
    nic_id      = optional(string)
    ip_ref      = optional(object({ subnet_id = string, address = string }))
    external_ip = optional(object({ address = string, zone_id = string }))
    weight      = optional(number)
  }))
  default = []

  validation {
    condition = alltrue([
      for t in var.targets : length([
        for v in [t.instance_id, t.nic_id, t.ip_ref, t.external_ip] : v if v != null
      ]) == 1
    ])
    error_message = "У каждой цели задаётся ровно одна форма адресации."
  }
}

variable "deregistration_delay" {
  description = <<-EOT
    Окно вывода цели из обслуживания.

    Умолчание модуля — `0s`, а НЕ умолчание края (`300s`): ресурсами здесь управляет
    Terraform, и каждое `destroy` ждало бы это окно. Ставьте больше осознанно.
  EOT
  type        = string
  default     = "0s"
}

variable "listeners" {
  description = <<-EOT
    Слушатели: ключ — суффикс имени, значение — порт и протокол.

    Backend-порта здесь нет и быть не может: он живёт на группе целей (`backend_port`
    модуля) и виден у слушателя эхом `resolved_backend_port`. Нужен другой — заводите
    отдельную группу.
  EOT
  type = map(object({
    protocol = optional(string, "TCP")
    port     = number
  }))
  default = {}
}

variable "labels" {
  description = "Метки, применяемые ко всем ресурсам модуля."
  type        = map(string)
  default     = {}
}
