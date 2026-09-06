# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: проверяют СВЯЗЫВАНИЕ и проверки входа, не обращаясь к краю.
# Провайдер подменён mock_provider — падение означает ошибку в модуле, а не состояние стенда.
#
# Отдельно про подмену — что она заменяет, а что оставляет настоящим.
#
# Здесь стояло «проверки самого провайдера (ValidateConfig) не исполняются, подменённый
# провайдер их не несёт». Это неверно, и проверено исполнением: настройка с пустым сужением
# (`resource_names = []`), которую модуль не проверяет, роняет прогон сообщением ПРОВАЙДЕРА
# «Пустое сужение задано явно». Подмена берёт у настоящего провайдера схему и проверку
# настройки; заменяются обращения к краю — создание, чтение, адъюдикация значений самим
# сервисом, — а вычисляемые поля заполняются сгенерёнными значениями.
#
# Следствие для отрицаний ниже: утверждать «поле осталось null» можно только о полях,
# которых схема НЕ вычисляет, и у каждого такого отрицания обязан стоять рядом
# положительный близнец — иначе оно зеленеет и на модуле, который поле вовсе не передаёт.

mock_provider "kaname" {}

variables {
  scope = { type = "iam.project", id = "prjprobe000000000000" }

  roles = {
    "net-operator" = {
      description = "правка сетей проекта"
      rules = [{
        module    = "vpc"
        resources = ["vpc.network", "vpc.subnet"]
        verbs     = ["get", "list", "update"]
      }]
    }
  }

  group_grants = {
    "net-operators" = {
      group_id = "grpprobe000000000000"
      role     = "net-operator"
      target   = { all_in_scope = true }
    }
  }
}

# Якорь задан ОДНОЙ переменной: роль, определённая на проекте, выдаётся только на своём
# проекте, и две переменные позволили бы задать расхождение, которое отвергнется на apply.
run "role_and_grant_share_one_anchor" {
  command = plan

  assert {
    condition = alltrue([
      kaname_role.this["net-operator"].definition_tier.tier_type == var.scope.type,
      kaname_role.this["net-operator"].definition_tier.tier_id == var.scope.id,
      kaname_access_binding.group["net-operators"].scope_type == var.scope.type,
      kaname_access_binding.group["net-operators"].scope_id == var.scope.id,
    ])
    error_message = "якорь разошёлся между определением роли и её выдачей"
  }
}

# Выдача ссылается на роль ПО ИДЕНТИФИКАТОРУ: ссылка строит граф, и снос идёт в обратном
# порядке — выдача раньше роли, которую она держит. Край не удаляет роль, на которую
# ссылается живая выдача, поэтому порядок здесь несущий.
run "grant_references_the_role_by_id" {
  command = plan

  assert {
    condition     = kaname_access_binding.group["net-operators"].role_id == kaname_role.this["net-operator"].id
    error_message = "выдача не связана с ролью модуля по идентификатору"
  }
}

# Удобный путь — групповой: вид принципала проставляет модуль, назвать здесь человека нечем.
run "group_grant_binds_a_group_subject" {
  command = plan

  assert {
    condition     = one(kaname_access_binding.group["net-operators"].subjects).type == "SUBJECT_TYPE_GROUP"
    error_message = "получателем групповой выдачи оказалась не группа"
  }
  assert {
    condition     = one(kaname_access_binding.group["net-operators"].subjects).id == "grpprobe000000000000"
    error_message = "идентификатор группы не доехал до выдачи"
  }
  # Ровно один получатель: состав выдачи у края неизменяем, и «добавить второго» означает
  # пересоздание привязки — то есть работу для группы, а не для перечисления.
  assert {
    condition     = length(kaname_access_binding.group["net-operators"].subjects) == 1
    error_message = "в групповой выдаче оказалось несколько получателей"
  }
}

# Значения собираются полем за полем, а не передаются переменной целиком. Проба закрепляет,
# что сборка ничего не теряет и ничего не добавляет от себя.
run "role_rules_survive_assembly" {
  command = plan

  variables {
    roles = {
      "net-operator" = {
        rules = [
          {
            module         = "vpc"
            resources      = ["vpc.network"]
            verbs          = ["get", "list"]
            resource_names = ["netprobe00000000000"]
          },
          {
            module    = "compute"
            resources = ["compute.instance"]
            verbs     = ["*"]
          },
          # Третье правило несёт ВТОРУЮ форму сужения — отбор по меткам. Оно стоит здесь
          # ради положительного близнеца к отрицанию «незаданное сужение не появилось»:
          # без него отрицание зеленело бы и на модуле, который match_labels не передаёт
          # вовсе (проверено обезоруживанием — вся суита оставалась зелёной).
          {
            module       = "storage"
            resources    = ["storage.volumes"]
            verbs        = ["get"]
            match_labels = { tier = "front" }
          },
        ]
      }
    }
  }

  assert {
    condition = alltrue([
      kaname_role.this["net-operator"].rules[0].module == "vpc",
      kaname_role.this["net-operator"].rules[0].verbs == tolist(["get", "list"]),
      one(kaname_role.this["net-operator"].rules[0].resource_names) == "netprobe00000000000",
    ])
    error_message = "правило роли потеряно при сборке"
  }
  # Порядок правил край сохраняет, поэтому второе правило обязано остаться вторым.
  assert {
    condition     = kaname_role.this["net-operator"].rules[1].module == "compute"
    error_message = "порядок правил не сохранён при сборке"
  }
  # Заданный отбор по меткам доезжает — положительный близнец к отрицанию ниже.
  assert {
    condition     = kaname_role.this["net-operator"].rules[2].match_labels["tier"] == "front"
    error_message = "отбор по меткам потерян при сборке"
  }
  # Незаданное сужение остаётся ОПУЩЕННЫМ: пустое значение — второе написание отсутствия,
  # край вернул бы его отсутствующим, и Terraform увидел бы расхождение на законной настройке.
  assert {
    condition     = kaname_role.this["net-operator"].rules[1].match_labels == null
    error_message = "незаданное сужение появилось из ниоткуда"
  }
}

# Обе ветви цели доезжают до привязки дословно.
run "target_branches_survive_assembly" {
  command = plan

  variables {
    group_grants = {
      "net-operators" = {
        group_id = "grpprobe000000000000"
        role     = "net-operator"
        target   = { all_in_scope = true }
      }
      "db-readers" = {
        group_id = "grpprobe000000000001"
        role     = "net-operator"
        target = {
          resources = [{ type = "vpc.network", id = "netprobe00000000000" }]
        }
      }
    }
  }

  assert {
    condition     = kaname_access_binding.group["net-operators"].target.all_in_scope == true
    error_message = "широчайшая ветвь цели не доехала до выдачи"
  }
  assert {
    condition     = one(kaname_access_binding.group["db-readers"].target.resources).id == "netprobe00000000000"
    error_message = "пообъектная ветвь цели потеряна при сборке"
  }
  # Ветви взаимоисключающи: у пообъектной выдачи широчайшая не появляется сама собой.
  assert {
    condition     = kaname_access_binding.group["db-readers"].target.all_in_scope == null
    error_message = "у пообъектной выдачи проставилась широчайшая ветвь"
  }
}

# Готовая роль — системная или определённая выше — берётся по идентификатору как есть, и
# своей роли модуль при этом не заводит.
run "external_role_is_taken_by_id" {
  command = plan

  variables {
    roles = {}
    group_grants = {
      "viewers" = {
        group_id = "grpprobe000000000002"
        role_id  = "rolprobe00000000000"
        target   = { all_in_scope = true }
      }
    }
  }

  assert {
    condition     = kaname_access_binding.group["viewers"].role_id == "rolprobe00000000000"
    error_message = "готовая роль не доехала до выдачи"
  }
  assert {
    condition     = length(kaname_role.this) == 0
    error_message = "модуль завёл роль, которой у него не просили"
  }
}

# Умолчание модуля — защита ВЫКЛЮЧЕНА, и это сказано явно, а не оставлено краю: включённая
# защита отвергает не только destroy, но и ЗАМЕНУ, а заменой у привязки является правка роли,
# получателя, цели или якоря.
#
# Рядом — положительный контроль: заданные срок и защита доезжают до выдачи. Без него
# утверждение об умолчании зеленело бы и на модуле, который эти поля просто не передаёт.
run "protection_defaults_to_off_and_explicit_values_reach_the_binding" {
  command = plan

  variables {
    group_grants = {
      "net-operators" = {
        group_id = "grpprobe000000000000"
        role     = "net-operator"
        target   = { all_in_scope = true }
      }
      "auditors" = {
        group_id            = "grpprobe000000000003"
        role                = "net-operator"
        target              = { all_in_scope = true }
        expires_at          = "2026-12-31T23:59:59Z"
        deletion_protection = true
      }
    }
  }

  assert {
    condition     = kaname_access_binding.group["net-operators"].deletion_protection == false
    error_message = "умолчание защиты от удаления не выключено — замена выдачи упёрлась бы в неё"
  }
  assert {
    condition     = kaname_access_binding.group["net-operators"].expires_at == null
    error_message = "невыданный срок появился из ниоткуда — бессрочная выдача стала срочной"
  }
  assert {
    condition = alltrue([
      kaname_access_binding.group["auditors"].deletion_protection == true,
      kaname_access_binding.group["auditors"].expires_at == "2026-12-31T23:59:59Z",
    ])
    error_message = "заданные срок и защита не доехали до выдачи"
  }
}

# Именная выдача — исключение: один получатель, обязательное обоснование, отдельный ресурс.
run "subject_grant_names_one_principal" {
  command = plan

  variables {
    subject_grants = {
      "ci-deployer" = {
        subject       = { type = "SUBJECT_TYPE_SERVICE_ACCOUNT", id = "svaprobe00000000000" }
        justification = "учётка конвейера сборки: получатель один, его личность и есть предмет выдачи"
        role          = "net-operator"
        target        = { all_in_scope = true }
      }
    }
  }

  assert {
    condition     = one(kaname_access_binding.subject["ci-deployer"].subjects).type == "SUBJECT_TYPE_SERVICE_ACCOUNT"
    error_message = "вид принципала именной выдачи не доехал"
  }
  assert {
    condition     = length(kaname_access_binding.subject["ci-deployer"].subjects) == 1
    error_message = "в именной выдаче оказалось несколько получателей"
  }
  assert {
    condition     = kaname_access_binding.subject["ci-deployer"].role_id == kaname_role.this["net-operator"].id
    error_message = "именная выдача не связана с ролью модуля по идентификатору"
  }
  # Обоснование не остаётся украшением настройки: все исключения установки видны одним выходом.
  assert {
    condition     = length(output.subject_grant_justifications["ci-deployer"]) > 0
    error_message = "обоснование именной выдачи не выведено наружу"
  }
}

run "labels_reach_every_resource" {
  command = plan

  variables {
    labels = { origin = "terraform" }
    subject_grants = {
      "ci-deployer" = {
        subject       = { type = "SUBJECT_TYPE_SERVICE_ACCOUNT", id = "svaprobe00000000000" }
        justification = "учётка конвейера сборки: получатель один и не изменится"
        role          = "net-operator"
        target        = { all_in_scope = true }
      }
    }
  }

  assert {
    condition = alltrue([
      kaname_role.this["net-operator"].labels["origin"] == "terraform",
      kaname_access_binding.group["net-operators"].labels["origin"] == "terraform",
      kaname_access_binding.subject["ci-deployer"].labels["origin"] == "terraform",
    ])
    error_message = "метки доехали не до всех ресурсов модуля"
  }
}

# ОТРИЦАНИЯ. Без них положительные пробы зеленели бы и на модуле, принимающем что угодно.

run "cluster_anchor_is_rejected" {
  command = plan

  variables {
    scope = { type = "iam.cluster", id = "cluster_kacho_root" }
  }

  expect_failures = [var.scope]
}

run "short_anchor_form_is_rejected" {
  command = plan

  variables {
    scope = { type = "project", id = "prjprobe000000000000" }
  }

  expect_failures = [var.scope]
}

run "role_without_rules_is_rejected" {
  command = plan

  variables {
    roles = { "empty-one" = { rules = [] } }
    # Выдача ссылается на роль по идентификатору: иначе упало бы предусловие о неизвестном
    # имени, и отрицание зеленело бы не на том, что проверяет.
    group_grants = {
      "viewers" = {
        group_id = "grpprobe000000000002"
        role_id  = "rolprobe00000000000"
        target   = { all_in_scope = true }
      }
    }
  }

  expect_failures = [var.roles]
}

run "rule_narrowed_twice_is_rejected" {
  command = plan

  variables {
    roles = {
      "net-operator" = {
        rules = [{
          module         = "vpc"
          resources      = ["vpc.network"]
          verbs          = ["get"]
          resource_names = ["netprobe00000000000"]
          match_labels   = { tier = "front" }
        }]
      }
    }
  }

  expect_failures = [var.roles]
}

run "grant_without_a_role_is_rejected" {
  command = plan

  variables {
    group_grants = {
      "net-operators" = {
        group_id = "grpprobe000000000000"
        target   = { all_in_scope = true }
      }
    }
  }

  expect_failures = [var.group_grants]
}

run "grant_naming_the_role_twice_is_rejected" {
  command = plan

  variables {
    group_grants = {
      "net-operators" = {
        group_id = "grpprobe000000000000"
        role     = "net-operator"
        role_id  = "rolprobe00000000000"
        target   = { all_in_scope = true }
      }
    }
  }

  expect_failures = [var.group_grants]
}

run "target_with_two_branches_is_rejected" {
  command = plan

  variables {
    group_grants = {
      "net-operators" = {
        group_id = "grpprobe000000000000"
        role     = "net-operator"
        target = {
          all_in_scope = true
          resources    = [{ type = "vpc.network", id = "netprobe00000000000" }]
        }
      }
    }
  }

  expect_failures = [var.group_grants]
}

run "target_without_a_branch_is_rejected" {
  command = plan

  variables {
    group_grants = {
      "net-operators" = {
        group_id = "grpprobe000000000000"
        role     = "net-operator"
        target   = {}
      }
    }
  }

  expect_failures = [var.group_grants]
}

# Ложь в all_in_scope не выражает ничего — и это ОТДЕЛЬНЫЙ запрет, не следствие правила
# «ровно одна ветвь».
#
# Здесь стоял одинокий `all_in_scope = false`, и проба была НЕРАЗЛИЧАЮЩЕЙ: такую цель
# отвергает и соседнее правило про единственную ветвь, поэтому проба зеленела и с
# обезоруженным запретом лжи (проверено обезоруживанием). Ложь рядом с непустым `resources`
# соседнее правило пропускает — красным остаётся ровно то, что проверяется.
run "false_all_in_scope_is_rejected" {
  command = plan

  variables {
    group_grants = {
      "net-operators" = {
        group_id = "grpprobe000000000000"
        role     = "net-operator"
        target = {
          all_in_scope = false
          resources    = [{ type = "vpc.network", id = "netprobe00000000000" }]
        }
      }
    }
  }

  expect_failures = [var.group_grants]
}

# Группа в карте исключений — обход осознанного шага: у неё есть основной вход, а две дороги
# к одному предмету разошлись бы.
run "group_in_subject_grants_is_rejected" {
  command = plan

  variables {
    subject_grants = {
      "net-operators" = {
        subject       = { type = "SUBJECT_TYPE_GROUP", id = "grpprobe000000000000" }
        justification = "так короче"
        role          = "net-operator"
        target        = { all_in_scope = true }
      }
    }
  }

  expect_failures = [var.subject_grants]
}

# Осознанный шаг — это непустое обоснование, а не наличие поля.
run "subject_grant_without_justification_is_rejected" {
  command = plan

  variables {
    subject_grants = {
      "ci-deployer" = {
        subject       = { type = "SUBJECT_TYPE_USER", id = "usrprobe00000000000" }
        justification = "   "
        role          = "net-operator"
        target        = { all_in_scope = true }
      }
    }
  }

  expect_failures = [var.subject_grants]
}

run "subject_grant_target_without_a_branch_is_rejected" {
  command = plan

  variables {
    subject_grants = {
      "ci-deployer" = {
        subject       = { type = "SUBJECT_TYPE_USER", id = "usrprobe00000000000" }
        justification = "получатель один и не изменится"
        role          = "net-operator"
        target        = {}
      }
    }
  }

  expect_failures = [var.subject_grants]
}

# Путь-исключение не мягче основного: тот же запрет лжи, и тоже в различающей форме.
run "false_all_in_scope_in_subject_grant_is_rejected" {
  command = plan

  variables {
    subject_grants = {
      "ci-deployer" = {
        subject       = { type = "SUBJECT_TYPE_USER", id = "usrprobe00000000000" }
        justification = "получатель один и не изменится"
        role          = "net-operator"
        target = {
          all_in_scope = false
          resources    = [{ type = "vpc.network", id = "netprobe00000000000" }]
        }
      }
    }
  }

  expect_failures = [var.subject_grants]
}

# Имя роли, которого у модуля нет, обязано отвергаться СВОИМ сообщением: иначе выдача уехала
# бы к краю с чужим идентификатором либо упала бы отказом про индекс, из которого не видно,
# что перепутаны role и role_id.
run "unknown_role_name_is_rejected" {
  command = plan

  variables {
    group_grants = {
      "net-operators" = {
        group_id = "grpprobe000000000000"
        role     = "no-such-role"
        target   = { all_in_scope = true }
      }
    }
  }

  expect_failures = [kaname_access_binding.group]
}
