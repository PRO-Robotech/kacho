# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: проверяют СВЯЗЫВАНИЕ и проверки входа, не обращаясь к краю.
# Провайдер подменён mock_provider — значит проба падает от ошибки в модуле, а не от
# состояния стенда.

mock_provider "kacho" {}

variables {
  account_id = "accprobe0000000000000"
  name       = "probe-project"
}

# Проект заводится в заданном аккаунте.
run "project_is_anchored_to_the_account" {
  command = plan

  assert {
    condition     = kacho_iam_project.this.account_id == var.account_id
    error_message = "проект не привязан к заданному аккаунту"
  }
  assert {
    condition     = kacho_iam_project.this.name == "probe-project"
    error_message = "имя проекта не доехало из переменной"
  }
}

# Группы и служебные учётки — АККАУНТНЫЕ, а не проектные.
run "groups_and_service_accounts_are_account_scoped" {
  command = plan

  variables {
    groups           = { "probe-admins" = "администраторы" }
    service_accounts = { "probe-ci" = "конвейер" }
  }

  # Первая редакция модуля заводила служебную учётку с полем проекта, которого у неё нет.
  # Проба закрепляет область: контракт принимает account_id у всех трёх ресурсов.
  assert {
    condition     = kacho_iam_group.this["probe-admins"].account_id == var.account_id
    error_message = "группа заведена не в аккаунте"
  }
  assert {
    condition     = kacho_iam_service_account.this["probe-ci"].account_id == var.account_id
    error_message = "служебная учётка заведена не в аккаунте"
  }
}

# Метки расходятся на все ресурсы модуля.
run "labels_reach_every_resource" {
  command = plan

  variables {
    labels           = { origin = "terraform" }
    groups           = { "probe-admins" = "администраторы" }
    service_accounts = { "probe-ci" = "конвейер" }
  }

  assert {
    condition = alltrue([
      kacho_iam_project.this.labels["origin"] == "terraform",
      kacho_iam_group.this["probe-admins"].labels["origin"] == "terraform",
      kacho_iam_service_account.this["probe-ci"].labels["origin"] == "terraform",
    ])
    error_message = "метки доехали не до всех ресурсов модуля"
  }
}
