# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: проверяют СВЯЗЫВАНИЕ и проверки входа, не обращаясь к краю.
# Провайдер подменён mock_provider — падение означает ошибку в модуле, а не состояние стенда.

mock_provider "kacho" {}

variables {
  project_id   = "prjprobe000000000000"
  region_id    = "ru-central1"
  name         = "probe"
  placement    = "INTERNAL_ZONAL"
  backend_port = 8080
  v4_source    = { subnet_id = "subprobe00000000000" }
}

# Регион задан ОДНОЙ переменной намеренно: край требует совпадения регионов группы и
# балансировщика, а две переменные позволили бы задать расхождение, которое обнаружится
# только на apply.
run "region_is_shared_by_group_and_balancer" {
  command = plan

  assert {
    condition = alltrue([
      kacho_nlb_target_group.this.region_id == var.region_id,
      kacho_nlb_load_balancer.this.region_id == var.region_id,
    ])
    error_message = "регион разошёлся между группой целей и балансировщиком"
  }
}

run "names_are_derived_from_one_base" {
  command = plan

  variables {
    listeners = { http = { port = 80, target_port = 8080 } }
  }

  assert {
    condition     = kacho_nlb_target_group.this.name == "probe-tg"
    error_message = "имя группы целей не выведено из основы"
  }
  assert {
    condition     = kacho_nlb_load_balancer.this.name == "probe"
    error_message = "имя балансировщика не равно основе"
  }
  assert {
    condition     = kacho_nlb_listener.this["http"].name == "probe-http"
    error_message = "имя слушателя не выведено из основы и ключа"
  }
}

# Слушатель ссылается на балансировщик и группу ПО ИДЕНТИФИКАТОРУ: ссылка строит граф, и
# снос идёт в обратном порядке — слушатели раньше группы, которую они держат. Край не
# удаляет группу, на которую ссылаются, поэтому порядок здесь несущий.
run "listener_references_both_by_id" {
  command = plan

  variables {
    listeners = { http = { port = 80, target_port = 8080 } }
  }

  assert {
    condition     = kacho_nlb_listener.this["http"].load_balancer_id == kacho_nlb_load_balancer.this.id
    error_message = "слушатель не связан с балансировщиком модуля"
  }
  assert {
    condition     = kacho_nlb_listener.this["http"].target_group_id == kacho_nlb_target_group.this.id
    error_message = "слушатель не связан с группой целей модуля"
  }
}

# Умолчание модуля — 0s, а НЕ краевые 300s: удаление цели двухфазное, и под управлением
# Terraform каждый destroy ждал бы пять минут.
run "deregistration_window_defaults_to_zero" {
  command = plan

  assert {
    condition     = kacho_nlb_target_group.this.deregistration_delay == "0s"
    error_message = "умолчание окна вывода не 0s — destroy будет ждать краевые 300s"
  }
}

# Значения собираются полем за полем, а не передаются переменной целиком: тип переменной
# фиксирован объявлением, а тип атрибута схемы включает вычисляемые поля. Проба
# закрепляет, что сборка ничего не теряет по дороге.
run "health_check_probe_survives_assembly" {
  command = plan

  variables {
    health_check = {
      interval = "7s"
      http     = { path = "/healthz", expected_codes = "200-299" }
    }
  }

  assert {
    condition     = kacho_nlb_target_group.this.health_check.http.path == "/healthz"
    error_message = "выбранная проба живости потеряна при сборке"
  }
  assert {
    condition     = kacho_nlb_target_group.this.health_check.interval == "7s"
    error_message = "период проверки потерян при сборке"
  }
  assert {
    condition     = kacho_nlb_target_group.this.health_check.tcp == null
    error_message = "незаданная проба появилась из ниоткуда"
  }
}

run "targets_survive_assembly" {
  command = plan

  variables {
    targets = [
      { external_ip = { address = "203.0.113.10", zone_id = "ru-central1-a" }, weight = 50 },
    ]
  }

  assert {
    condition     = one(kacho_nlb_target_group.this.targets).external_ip.address == "203.0.113.10"
    error_message = "цель потеряна при сборке"
  }
  assert {
    condition     = one(kacho_nlb_target_group.this.targets).weight == 50
    error_message = "вес цели потерян при сборке"
  }
}

# ОТРИЦАНИЯ. Без них положительные пробы зеленели бы и на модуле, принимающем что угодно.

run "unknown_placement_is_rejected" {
  command = plan

  variables {
    placement = "INTERNAL"
  }

  expect_failures = [var.placement]
}

run "two_health_probes_are_rejected" {
  command = plan

  variables {
    health_check = {
      tcp  = {}
      http = { path = "/healthz" }
    }
  }

  expect_failures = [var.health_check]
}

run "no_health_probe_is_rejected" {
  command = plan

  variables {
    health_check = { interval = "5s" }
  }

  expect_failures = [var.health_check]
}

run "target_with_two_identities_is_rejected" {
  command = plan

  variables {
    targets = [
      {
        instance_id = "insprobe00000000000"
        external_ip = { address = "203.0.113.10", zone_id = "ru-central1-a" }
      },
    ]
  }

  expect_failures = [var.targets]
}

run "vip_source_with_both_fields_is_rejected" {
  command = plan

  variables {
    v4_source = { subnet_id = "subprobe00000000000", address_id = "adrprobe00000000000" }
  }

  expect_failures = [var.v4_source]
}

run "empty_vip_source_is_rejected" {
  command = plan

  variables {
    v4_source = {}
  }

  expect_failures = [var.v4_source]
}
