# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: проверяют СВЯЗЫВАНИЕ и проверки входа, не обращаясь к краю.
# Провайдер подменён mock_provider — значит проба падает от ошибки в модуле, а не от
# состояния стенда.

mock_provider "kaname" {}

variables {
  account_id = "accprobe0000000000000"
  name       = "probe-project"
}

# Проект заводится в заданном аккаунте.
run "project_is_anchored_to_the_account" {
  command = plan

  assert {
    condition     = kaname_project.this.account_id == var.account_id
    error_message = "проект не привязан к заданному аккаунту"
  }
  assert {
    condition     = kaname_project.this.name == "probe-project"
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
    condition     = kaname_group.this["probe-admins"].account_id == var.account_id
    error_message = "группа заведена не в аккаунте"
  }
  assert {
    condition     = kaname_service_account.this["probe-ci"].account_id == var.account_id
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
      kaname_project.this.labels["origin"] == "terraform",
      kaname_group.this["probe-admins"].labels["origin"] == "terraform",
      kaname_service_account.this["probe-ci"].labels["origin"] == "terraform",
    ])
    error_message = "метки доехали не до всех ресурсов модуля"
  }
}

# ПРИГЛАШЕНИЯ.
#
# Законный близнец для всех отрицаний ниже — `{ lead = { email = "lead@probe.test" } }`:
# каждый негатив отличается от него РОВНО ОДНИМ признаком, тем, который и проверяет.
# Иначе проба зеленела бы по чужой причине — карта, нарушающая сразу три проверки,
# отвергается любой из них, и снятие проверяемой осталось бы незамеченным.

# Приглашение — АККАУНТНОЕ: строка членства принадлежит аккаунту, а не проекту.
run "invitation_is_anchored_to_the_account" {
  command = plan

  variables {
    invitations = {
      lead = { email = "lead@probe.test", display_name = "Ведущий" }
    }
  }

  assert {
    condition     = kaname_user_invitation.this["lead"].account_id == var.account_id
    error_message = "приглашение заведено не в аккаунте модуля"
  }
  # Почта берётся ЗНАЧЕНИЕМ, а ключ остаётся псевдонимом: если однажды кто-то решит
  # «ключ и есть почта», эта пара утверждений разойдётся первой.
  assert {
    condition     = kaname_user_invitation.this["lead"].email == "lead@probe.test"
    error_message = "почта не доехала из значения карты"
  }
  assert {
    condition     = kaname_user_invitation.this["lead"].display_name == "Ведущий"
    error_message = "затравка отображаемого имени потеряна при сборке"
  }
}

# Стартовая выдача ссылается на проект ЭТОГО модуля ПО ИДЕНТИФИКАТОРУ — ссылка строит граф
# (создание проект → приглашение, снос в обратном порядке). Утверждение несущее и это
# проверено подстановкой: модуль, отдавший сюда ЛЮБОЙ другой идентификатор, провайдером
# принимается (пара «проект + роль» цела), и краснеет ровно эта проба, своим текстом.
#
# Рядом, в том же прогоне, приглашение БЕЗ роли: проект ему не подставляется. Вторая
# ветка проверяется тем же прогоном, но краснеет ЧУЖИМ текстом: подставленный проект без
# роли отвергает сам провайдер («Стартовая выдача задана наполовину») раньше, чем дело
# доходит до утверждения. Сказано здесь оно всё равно — иначе вторая ветка условия не
# названа нигде, и следующий читатель примет её за упущение.
run "starting_grant_points_at_the_module_project" {
  command = plan

  variables {
    invitations = {
      lead   = { email = "lead@probe.test", role_id = "roleprobe0000000000" }
      viewer = { email = "viewer@probe.test" }
    }
  }

  assert {
    condition     = kaname_user_invitation.this["lead"].project_id == kaname_project.this.id
    error_message = "стартовая выдача не связана с проектом модуля по идентификатору"
  }
  assert {
    condition     = kaname_user_invitation.this["lead"].role_id == "roleprobe0000000000"
    error_message = "роль стартовой выдачи потеряна при сборке"
  }
  assert {
    condition     = kaname_user_invitation.this["viewer"].project_id == null
    error_message = "приглашению без роли подставлен проект — к краю уедет половина пары «проект + роль»"
  }
}

# Метки модуля доезжают и до строки членства.
run "labels_reach_the_invitation" {
  command = plan

  variables {
    labels      = { origin = "terraform" }
    invitations = { lead = { email = "lead@probe.test" } }
  }

  assert {
    condition     = kaname_user_invitation.this["lead"].labels["origin"] == "terraform"
    error_message = "метки модуля не доехали до строки членства"
  }
}

# Запрет входа доезжает до ресурса. Примет ли его край — зависит от состояния строки
# (непринятое приглашение блокировать нельзя); проба утверждает только сборку модуля.
#
# Проба несущая, хотя атрибут вычисляем: снятие строки из модуля роняет её (проверено).
# Обратное было бы правдоподобно — у соседнего модуля записано, что утверждения о
# вычисляемых полях при подменённом провайдере ничего не значат, — но здесь поле ещё и
# необязательное, и на плане у него остаётся заданное значение, а не сгенерированное.
run "blocked_request_reaches_the_row" {
  command = plan

  variables {
    invitations = { lead = { email = "lead@probe.test", blocked = true } }
  }

  assert {
    condition     = kaname_user_invitation.this["lead"].blocked == true
    error_message = "запрет входа не доехал до строки членства"
  }
}

# ОТРИЦАНИЯ. Без них положительные пробы зеленели бы и на модуле, принимающем что угодно.
# Ни одно сообщение проверок не называет почту — только ключ карты: почта это персональные
# данные, а текст отказа уезжает в журналы и обзоры изменений.

run "invitation_without_email_is_rejected" {
  command = plan

  variables {
    invitations = { lead = { email = "" } }
  }

  expect_failures = [var.invitations]
}

# Два псевдонима с одной почтой легли бы поверх ОДНОЙ строки членства: приглашение
# идемпотентно по паре «аккаунт + почта». Регистр здесь разный намеренно — край сличает
# почту без его учёта, и проверка обязана делать то же.
run "invitations_sharing_one_email_are_rejected" {
  command = plan

  variables {
    invitations = {
      lead   = { email = "lead@probe.test" }
      deputy = { email = "Lead@Probe.Test" }
    }
  }

  expect_failures = [var.invitations]
}

# Пустая строка — не «ничего»: модуль подставит проект, и к краю уедет проект без роли.
run "invitation_with_empty_role_id_is_rejected" {
  command = plan

  variables {
    invitations = { lead = { email = "lead@probe.test", role_id = "" } }
  }

  expect_failures = [var.invitations]
}

# Ключ карты становится адресом ресурса и печатается в плане, журнале и состоянии —
# почтой адресовать записи нельзя.
run "email_shaped_key_is_rejected" {
  command = plan

  variables {
    invitations = { "lead@probe.test" = { email = "lead@probe.test" } }
  }

  expect_failures = [var.invitations]
}
