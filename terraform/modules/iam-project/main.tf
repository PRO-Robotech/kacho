# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

terraform {
  required_version = ">= 1.6"
  required_providers {
    kacho = {
      source = "PRO-Robotech/kacho"
    }
  }
}

resource "kacho_iam_project" "this" {
  account_id  = var.account_id
  name        = var.name
  description = var.description
  labels      = var.labels
}

# Группы живут в АККАУНТЕ, а не в проекте: одна и та же группа обычно получает права в
# нескольких проектах, и завести её внутри проекта значило бы плодить одноимённые группы,
# состав которых разъедется.
resource "kacho_iam_group" "this" {
  for_each = var.groups

  account_id  = var.account_id
  name        = each.key
  description = each.value
  labels      = var.labels
}

# Служебные учётки — тоже АККАУНТНЫЕ: контракт принимает account_id, поля проекта у них
# нет. Сузить учётку до проекта можно только выдачей прав, а не местом заведения.
resource "kacho_iam_service_account" "this" {
  for_each = var.service_accounts

  account_id  = var.account_id
  name        = each.key
  description = each.value
  labels      = var.labels
}
