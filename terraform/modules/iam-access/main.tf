# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

terraform {
  required_version = ">= 1.6"
  required_providers {
    kaname = {
      source = "PRO-Robotech/kacho"
    }
  }
}

locals {
  # Идентификаторы ролей, определённых ЭТИМ модулем, по их именам. Выдача, назвавшая роль
  # по имени, резолвится через эту карту — то есть ссылается на идентификатор, а не на имя.
  own_role_ids = { for k, r in kaname_role.this : k => r.id }
}

resource "kaname_role" "this" {
  for_each = var.roles

  # Уровень определения роли — тот же якорь, что у выдач модуля: роль, определённая на
  # проекте, выдаётся только на своём проекте.
  definition_tier = {
    tier_type = var.scope.type
    tier_id   = var.scope.id
  }

  name        = each.key
  description = each.value.description
  labels      = var.labels

  # Значения собираются ПОЛЕ ЗА ПОЛЕМ, а не передаются переменной целиком.
  #
  # Тип переменной модуля фиксирован её объявлением, а тип атрибута схемы свой: где-то это
  # набор против списка, где-то в него входят ещё и вычисляемые поля, которых во входной
  # переменной нет и быть не должно. Передача переменной целиком даёт «Value Conversion
  # Error» — отказ, чей текст не называет ни поля, ни причины. Литерал же Terraform приводит
  # к типу схемы сам, дополняя недостающее null.
  rules = [
    for rule in each.value.rules : {
      module         = rule.module
      resources      = rule.resources
      verbs          = rule.verbs
      resource_names = rule.resource_names
      match_labels   = rule.match_labels
    }
  ]
}

# Групповая выдача — ОСНОВНОЙ путь модуля.
#
# Привязка ссылается на роль ПО ИДЕНТИФИКАТОРУ, а не по имени: ссылка строит граф, и снос
# идёт в обратном порядке — выдачи раньше роли, которую они держат. Порядок здесь несущий:
# роль, на которую ссылается живая выдача, край удалять не станет.
resource "kaname_access_binding" "group" {
  for_each = var.group_grants

  role_id = each.value.role_id != null ? each.value.role_id : local.own_role_ids[each.value.role]

  scope_type = var.scope.type
  scope_id   = var.scope.id

  # Вид принципала проставляет МОДУЛЬ, а не вызывающий: у этого входа он один по построению,
  # и поле для него означало бы, что здесь же можно выдать право лично — то есть обойти
  # осознанный шаг, ради которого заведён отдельный вход subject_grants.
  subjects = [{
    type = "SUBJECT_TYPE_GROUP"
    id   = each.value.group_id
  }]

  target = {
    all_in_scope = each.value.target.all_in_scope
    resources = each.value.target.resources == null ? null : [
      for r in each.value.target.resources : { type = r.type, id = r.id }
    ]
  }

  expires_at = each.value.expires_at

  # Умолчание модуля — защита ВЫКЛЮЧЕНА, и это сказано явно, а не оставлено краю.
  # Здесь ресурсами управляет Terraform, а включённая защита отвергает не только `destroy`,
  # но и ЗАМЕНУ — заменой же у привязки является правка роли, получателя, цели или якоря
  # (край не принимает эти поля в маске изменения). Включив её, снимайте отдельным apply
  # прежде, чем менять остальное.
  deletion_protection = each.value.deletion_protection

  labels = var.labels

  lifecycle {
    precondition {
      condition     = each.value.role == null || contains(keys(var.roles), each.value.role)
      error_message = "Роль по имени не найдена среди ролей модуля. Поле role называет роль ИЗ ЭТОГО модуля (ключ в var.roles); готовая роль — системная или определённая выше — задаётся идентификатором в role_id."
    }
  }
}

# Именная выдача — ИСКЛЮЧЕНИЕ, отдельным ресурсом, а не веткой основного.
#
# Разные ресурсы, а не один с объединённой картой: так перечень исключений установки виден
# и в плане, и в состоянии одним адресом, а обзор «кому у нас выдано лично» не требует
# разбирать содержимое каждой записи.
resource "kaname_access_binding" "subject" {
  for_each = var.subject_grants

  role_id = each.value.role_id != null ? each.value.role_id : local.own_role_ids[each.value.role]

  scope_type = var.scope.type
  scope_id   = var.scope.id

  # Получатель ровно один: вторым здесь быть нечему — форма входа его не несёт.
  subjects = [{
    type = each.value.subject.type
    id   = each.value.subject.id
  }]

  target = {
    all_in_scope = each.value.target.all_in_scope
    resources = each.value.target.resources == null ? null : [
      for r in each.value.target.resources : { type = r.type, id = r.id }
    ]
  }

  expires_at          = each.value.expires_at
  deletion_protection = each.value.deletion_protection

  labels = var.labels

  lifecycle {
    precondition {
      condition     = each.value.role == null || contains(keys(var.roles), each.value.role)
      error_message = "Роль по имени не найдена среди ролей модуля. Поле role называет роль ИЗ ЭТОГО модуля (ключ в var.roles); готовая роль — системная или определённая выше — задаётся идентификатором в role_id."
    }
  }
}
