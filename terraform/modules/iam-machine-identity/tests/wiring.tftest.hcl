# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: проверяют СВЯЗЫВАНИЕ и проверки входа, не обращаясь к краю.
# Провайдер подменён mock_provider — падение означает ошибку в модуле, а не состояние стенда.
#
# Материала ключа пробы не касаются: под подменённым провайдером он случайный, и любое
# утверждение о нём говорило бы о подмене, а не о модуле.

mock_provider "kaname" {}

variables {
  account_id         = "accprobe0000000000000"
  name               = "probe-machine"
  created_by_user_id = "usrprobe0000000000000"
}

# Учётка и ключ приезжают ВМЕСТЕ — ради этого модуль и заведён: учётка без ключа выглядит
# готовой, будучи личностью, которой нечем войти.
run "account_and_key_arrive_together" {
  command = plan

  assert {
    condition     = kaname_service_account.this.account_id == var.account_id
    error_message = "учётка заведена не в заданном аккаунте"
  }
  assert {
    condition     = length(kaname_service_account_key.this) == 1
    error_message = "умолчание не выдало учётке ни одного ключа — войти ею нечем"
  }
  assert {
    condition     = kaname_service_account_key.this["default"].name == "probe-machine-default"
    error_message = "имя ключа не выведено из основы и ключа записи"
  }
}

# Ключ ссылается на учётку ПО ИДЕНТИФИКАТОРУ: ссылка строит граф, снос идёт в обратном
# порядке — ключи раньше учётки. Без ссылки отзыв ключа ушёл бы по адресу владельца,
# которого уже нет.
run "key_references_the_service_account_by_id" {
  command = plan

  assert {
    condition     = kaname_service_account_key.this["default"].service_account_id == kaname_service_account.this.id
    error_message = "ключ не связан с учёткой модуля"
  }
  assert {
    condition     = kaname_service_account_key.this["default"].created_by_user_id == var.created_by_user_id
    error_message = "ответственный за выпуск не доехал до ключа"
  }
}

# Умолчание модуля — 90 дней, и оно названо ЗДЕСЬ, а не оставлено краю. Краевой ноль — это
# «умолчание установки», а не «бессрочно»: величина конечна, но задаётся настройкой той
# установки, куда пришёл apply, и в конфигурации не видна. Явное число делает исход
# независимым от настройки — включая установку, где умолчание не настроено вовсе и ноль
# действительно означает ключ без срока.
run "key_ttl_defaults_to_a_finite_window" {
  command = plan

  assert {
    condition     = kaname_service_account_key.this["default"].ttl_seconds == 7776000
    error_message = "умолчание срока не 90 дней — ключ достался бы бессрочным по невнимательности"
  }
}

# Значения записи собираются полем за полем; проба закрепляет, что сборка ничего не теряет
# и что null у срока означает умолчание МОДУЛЯ, а не края.
run "per_key_values_survive_assembly" {
  command = plan

  variables {
    key_ttl_seconds = 2592000
    keys = {
      current = { description = "действующий" }
      next    = { description = "на смену", ttl_seconds = 604800, audience = ["kacho"] }
    }
  }

  assert {
    condition     = kaname_service_account_key.this["current"].ttl_seconds == 2592000
    error_message = "незаданный срок записи не унаследовал умолчание модуля"
  }
  assert {
    condition     = kaname_service_account_key.this["next"].ttl_seconds == 604800
    error_message = "заданный срок записи не перекрыл умолчание модуля"
  }
  assert {
    condition     = kaname_service_account_key.this["next"].description == "на смену"
    error_message = "описание ключа потеряно при сборке"
  }
  assert {
    condition     = kaname_service_account_key.this["next"].audience[0] == "kacho"
    error_message = "назначение токена потеряно при сборке"
  }
  # Оба ключа висят на ОДНОЙ учётке: смена ключа требует двух живых одновременно, и вторая
  # учётка вместо второго ключа сменой не является.
  assert {
    condition = alltrue([
      kaname_service_account_key.this["current"].service_account_id == kaname_service_account.this.id,
      kaname_service_account_key.this["next"].service_account_id == kaname_service_account.this.id,
    ])
    error_message = "ключи смены висят на разных учётках"
  }
}

run "labels_reach_the_account_and_every_key" {
  command = plan

  variables {
    labels = { origin = "terraform" }
    keys = {
      current = {}
      next    = {}
    }
  }

  assert {
    condition = alltrue([
      kaname_service_account.this.labels["origin"] == "terraform",
      kaname_service_account_key.this["current"].labels["origin"] == "terraform",
      kaname_service_account_key.this["next"].labels["origin"] == "terraform",
    ])
    error_message = "метки доехали не до всех ресурсов модуля"
  }
}

# ОТРИЦАНИЯ. Без них положительные пробы зеленели бы и на модуле, принимающем что угодно.

# Предмет модуля: учётка без ключа не заводится этим объявлением вовсе.
run "service_account_without_a_key_is_rejected" {
  command = plan

  variables {
    keys = {}
  }

  expect_failures = [var.keys]
}

run "empty_name_is_rejected" {
  command = plan

  variables {
    name = "   "
  }

  expect_failures = [var.name]
}

# Полоса, ради которой машинная личность и заводится: КОНВЕЙЕР применяет модуль СЛУЖЕБНОЙ
# УЧЁТКОЙ и ответственного не называет — назвать он не вправе никого. Прежняя редакция
# этой пробы требовала обратного (пустое отвергалось) и закрепляла ровно тот дефект,
# из-за которого модуль был неприменим тем, кому адресован.
run "machine_caller_needs_no_issuer" {
  command = plan

  variables {
    created_by_user_id = null
  }

  assert {
    condition     = length(kaname_service_account_key.this) == 1
    error_message = "без названного ответственного план не собрался — конвейер модуль не применит"
  }
  assert {
    condition     = kaname_service_account_key.this["default"].name == "probe-machine-default"
    error_message = "ключ собран не из основы и ключа записи"
  }
}

# Отрицание к полосе выше: ПУСТАЯ СТРОКА — не «не назвал».
#
# На провод она уезжает неотличимо от отсутствия, край подставит своё, а настройка
# продолжит утверждать пустоту: применение кончится отказом, называющим ошибкой ПРОВАЙДЕРА
# чужую опечатку. Отказ обязан приходить на plan и называть переменную.
run "empty_issuer_string_is_rejected" {
  command = plan

  variables {
    created_by_user_id = ""
  }

  expect_failures = [var.created_by_user_id]
}

# Потолок — год: столько включительно допускает развёрнутый край. Проверка переносит отказ
# с apply на plan, иначе о превышении узнаёшь после того, как учётка уже создана.
#
# Вход взят на секунду выше потолка НАМЕРЕННО: прежняя редакция отвергала 63072001 —
# контрактную границу, вдвое большую краевой, — и потому оставалась зелёной на всей полосе
# от года до двух лет, которую край отвергает, а plan пропускал.
run "ttl_above_the_edge_ceiling_is_rejected" {
  command = plan

  variables {
    key_ttl_seconds = 31536001
  }

  expect_failures = [var.key_ttl_seconds]
}

# Положительный контроль к отрицанию выше: потолок ВКЛЮЧИТЕЛЬНЫЙ, и ровно потолок проходит.
# Без этой пробы отрицание зеленело бы и на проверке, отвергающей заодно законный вход.
run "ttl_exactly_at_the_edge_ceiling_is_accepted" {
  command = plan

  variables {
    key_ttl_seconds = 31536000
  }

  assert {
    condition     = kaname_service_account_key.this["default"].ttl_seconds == 31536000
    error_message = "ровно потолок отвергнут — проверка режет законный вход"
  }
}

run "negative_ttl_is_rejected" {
  command = plan

  variables {
    key_ttl_seconds = -1
  }

  expect_failures = [var.key_ttl_seconds]
}

# Дробный срок тип `number` принимает, а край берёт целые секунды: без проверки отказ
# пришёл бы преобразованием типа, не называющим ни поля, ни причины.
run "fractional_ttl_is_rejected" {
  command = plan

  variables {
    key_ttl_seconds = 1.5
  }

  expect_failures = [var.key_ttl_seconds]
}

run "per_key_ttl_above_the_edge_ceiling_is_rejected" {
  command = plan

  variables {
    keys = {
      too_long = { ttl_seconds = 31536001 }
    }
  }

  expect_failures = [var.keys]
}

# Тот же включительный потолок и у записи: проверка записи и проверка умолчания — разные
# предикаты, и разойтись они могут молча.
run "per_key_ttl_exactly_at_the_edge_ceiling_is_accepted" {
  command = plan

  variables {
    keys = {
      at_ceiling = { ttl_seconds = 31536000 }
    }
  }

  assert {
    condition     = kaname_service_account_key.this["at_ceiling"].ttl_seconds == 31536000
    error_message = "ровно потолок отвергнут у записи — проверка записи расходится с проверкой умолчания"
  }
}
