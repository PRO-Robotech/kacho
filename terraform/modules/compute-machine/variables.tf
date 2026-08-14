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

    Значение — дискриминатор ВЛАДЕЛЬЦА (`storage.image` либо `registry.image`),
    а не вид ресурса. Здесь стоял набор `IMAGE`/`SNAPSHOT`/`VOLUME`, которого край
    не принимает ни одним значением: модуль отвергал всё законное и пропускал
    только незаконное. Молчала и проба модуля — она утверждала тот же неверный
    набор, то есть закрепляла дефект вместо того, чтобы его ловить.
  EOT
  type = map(object({
    name               = string
    description        = optional(string)
    machine_type_id    = string
    boot_source_type   = string
    boot_source_id     = string
    subnet_id          = string
    security_group_ids = optional(list(string))

    # Группа размещения: ЛИБО заведённая этим модулем (ключ из `placement_groups`),
    # ЛИБО чужая (идентификатор). Два поля вместо одного — потому что это два
    # разных вопроса: «какую из моих» и «какую снаружи». Ровно одно из них
    # задаётся; проверка ниже это держит.
    placement_group_key = optional(string)
    placement_group_id  = optional(string)

    # Ключи входа СКЛАДЫВАЮТСЯ: набор ключей — множество, и у машины законно
    # бывают и заведённые этим модулем, и чужие.
    guest_access_key_keys = optional(list(string))
    guest_access_key_ids  = optional(list(string))

    service_account_id = optional(string)
  }))

  validation {
    condition = alltrue([
      for m in values(var.machines) :
      contains(["storage.image", "registry.image"], m.boot_source_type)
    ])
    error_message = "boot_source_type обязан быть storage.image либо registry.image — это дискриминатор ВЛАДЕЛЬЦА источника, а не вид ресурса."
  }

  validation {
    condition     = alltrue([for m in values(var.machines) : m.boot_source_id != ""])
    error_message = "boot_source_id обязателен: машина не заводится без источника загрузки."
  }

  validation {
    condition = alltrue([
      for m in values(var.machines) :
      !(m.placement_group_key != null && m.placement_group_id != null)
    ])
    error_message = "placement_group_key и placement_group_id взаимоисключающи: группа либо заводится этим модулем, либо приходит снаружи."
  }
}

variable "guest_access_keys" {
  description = <<-EOT
    Ключи входа гостя, заводимые вместе с набором. Ключ карты — ссылка для
    `machines[*].guest_access_key_keys`.

    Принимается ТОЛЬКО публичная половина. Закрытая сюда не передаётся никогда:
    состояние Terraform хранится открытым текстом, и закрытый ключ в нём означал
    бы, что доступ к файлу состояния равен доступу в машину.
  EOT
  type = map(object({
    name       = string
    public_key = string
  }))
  default = {}

  validation {
    condition     = alltrue([for k in values(var.guest_access_keys) : k.public_key != ""])
    error_message = "public_key обязателен: ключ без материала не даёт входа никуда."
  }

  # Отрицание в паре с предметом: строка, похожая на закрытую половину, отвергается
  # ЗДЕСЬ — до того, как уедет в состояние. Провайдер её тоже не примет, но там она
  # уже будет записана в план.
  validation {
    condition = alltrue([
      for k in values(var.guest_access_keys) :
      !can(regex("PRIVATE KEY", k.public_key))
    ])
    error_message = "В public_key передана закрытая половина ключа. Сюда идёт ТОЛЬКО публичная: состояние Terraform хранится открытым текстом."
  }
}

variable "placement_groups" {
  description = <<-EOT
    Правила взаимного размещения, заводимые вместе с набором. Ключ карты — ссылка
    для `machines[*].placement_group_key`.

    Якорь взаимоисключающий: `zone_id` ЛИБО `region_id`, ровно один.
  EOT
  type = map(object({
    name        = string
    description = optional(string)
    strategy    = string
    zone_id     = optional(string)
    region_id   = optional(string)
  }))
  default = {}

  validation {
    condition = alltrue([
      for g in values(var.placement_groups) : contains(["SPREAD", "PACK"], g.strategy)
    ])
    error_message = "strategy обязана быть SPREAD (разнести) либо PACK (сблизить)."
  }

  validation {
    condition = alltrue([
      for g in values(var.placement_groups) :
      (g.zone_id != null) != (g.region_id != null)
    ])
    error_message = "Задаётся ровно одна координата якоря: zone_id ЛИБО region_id."
  }
}
