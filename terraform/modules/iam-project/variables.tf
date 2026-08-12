# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

variable "account_id" {
  description = "Аккаунт-владелец. Сменить его у существующего проекта нельзя."
  type        = string
}

variable "name" {
  description = <<-EOT
    Имя проекта в пределах аккаунта. Обязательно.

    Провайдер требует имя строже, чем край: по нему он находит уже созданный ресурс, если
    ответ на создание потерялся. Без имени повтор создал бы дубль.
  EOT
  type        = string
}

variable "description" {
  description = "Произвольное описание проекта."
  type        = string
  default     = ""
}

variable "labels" {
  description = "Метки, применяемые ко всем ресурсам модуля."
  type        = map(string)
  default     = {}
}

variable "groups" {
  description = <<-EOT
    Группы, заводимые в аккаунте: ключ — имя, значение — описание.

    Группы, а не перечисление людей: право, которое получает больше одного принципала или
    чей состав может измениться, выдаётся ГРУППЕ. Снять одного из группы — одна запись
    членства; снять одного из перечисления — снятие прав по всем объектам роли.
  EOT
  type        = map(string)
  default     = {}
}

variable "service_accounts" {
  description = <<-EOT
    Служебные учётки: ключ — имя, значение — описание.

    Заводятся В АККАУНТЕ, а не в проекте — так устроен контракт (`CreateServiceAccount`
    принимает `account_id`). Сужение до проекта делается ВЫДАЧЕЙ прав на этот проект, а
    не местом заведения учётки.
  EOT
  type        = map(string)
  default     = {}
}
