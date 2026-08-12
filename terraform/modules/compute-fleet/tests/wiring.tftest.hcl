# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: связывание и проверки входа, без обращения к краю.

mock_provider "kacho" {}

variables {
  project_id = "prjprobe000000000000"
  zone_id    = "ru-central1-a"
  instances = {
    "probe-app" = {
      machine_type_id         = "standard-2"
      boot_source             = { type = "image", id = "imgprobe0000000000" }
      acknowledge_unreachable = true
    }
  }
}

# Зона задана ОДНОЙ переменной на набор: расхождение зон машины и подсети отвергает край,
# и одна переменная хотя бы не даёт задать его внутри набора.
run "zone_is_shared_by_the_whole_fleet" {
  command = plan

  variables {
    instances = {
      "a" = { machine_type_id = "standard-2", boot_source = { type = "image", id = "img1" }, acknowledge_unreachable = true }
      "b" = { machine_type_id = "standard-4", boot_source = { type = "image", id = "img1" }, acknowledge_unreachable = true }
    }
  }

  assert {
    condition = alltrue([
      kacho_compute_instance.this["a"].zone_id == var.zone_id,
      kacho_compute_instance.this["b"].zone_id == var.zone_id,
    ])
    error_message = "зона разошлась между машинами набора"
  }
}

run "name_comes_from_the_map_key" {
  command = plan

  assert {
    condition     = kacho_compute_instance.this["probe-app"].name == "probe-app"
    error_message = "имя машины берётся не из ключа карты"
  }
}

# Источник загрузки собирается полем за полем — проба закрепляет, что сборка ничего не
# теряет по дороге.
run "boot_source_survives_assembly" {
  command = plan

  assert {
    condition = alltrue([
      kacho_compute_instance.this["probe-app"].boot_source.type == "image",
      kacho_compute_instance.this["probe-app"].boot_source.id == "imgprobe0000000000",
    ])
    error_message = "источник загрузки потерян при сборке"
  }
}

run "interfaces_survive_assembly" {
  command = plan

  variables {
    instances = {
      "probe-app" = {
        machine_type_id         = "standard-2"
        boot_source             = { type = "image", id = "img1" }
        acknowledge_unreachable = true
        network_interfaces = [
          { subnet_id = "subprobe000000000", security_group_ids = ["sgprobe00000000000"] },
        ]
      }
    }
  }

  assert {
    condition     = one(kacho_compute_instance.this["probe-app"].network_interface_specs).subnet_id == "subprobe000000000"
    error_message = "подсеть интерфейса потеряна при сборке"
  }
}

run "labels_of_the_fleet_and_of_the_instance_merge" {
  command = plan

  variables {
    labels = { origin = "terraform" }
    instances = {
      "probe-app" = {
        machine_type_id         = "standard-2"
        boot_source             = { type = "image", id = "img1" }
        acknowledge_unreachable = true
        labels                  = { role = "api" }
      }
    }
  }

  assert {
    condition = alltrue([
      kacho_compute_instance.this["probe-app"].labels["origin"] == "terraform",
      kacho_compute_instance.this["probe-app"].labels["role"] == "api",
    ])
    error_message = "метки набора и машины не слились"
  }
}

# ОТРИЦАНИЯ. Без них положительные пробы зеленели бы и на модуле, принимающем что угодно.

run "unknown_boot_source_type_is_rejected" {
  command = plan

  variables {
    instances = {
      "bad" = { machine_type_id = "standard-2", boot_source = { type = "disk", id = "x" } }
    }
  }

  expect_failures = [var.instances]
}

run "empty_boot_source_id_is_rejected" {
  command = plan

  variables {
    instances = {
      "bad" = { machine_type_id = "standard-2", boot_source = { type = "image", id = "  " } }
    }
  }

  expect_failures = [var.instances]
}

run "empty_machine_type_is_rejected" {
  command = plan

  variables {
    instances = {
      "bad" = { machine_type_id = "", boot_source = { type = "image", id = "img1" } }
    }
  }

  expect_failures = [var.instances]
}

run "interface_without_subnet_is_rejected" {
  command = plan

  variables {
    instances = {
      "bad" = {
        machine_type_id         = "standard-2"
        boot_source             = { type = "image", id = "img1" }
        acknowledge_unreachable = true
        network_interfaces      = [{ subnet_id = "" }]
      }
    }
  }

  expect_failures = [var.instances]
}

# Отрицание на самого стража: машина вида VM без выбора достижимости отвергается ВХОДОМ,
# не краем. Без этой пробы проверка молча исчезла бы вместе со стражем.
run "vm_without_reachability_choice_is_rejected" {
  command = plan

  variables {
    instances = {
      "bad" = { machine_type_id = "standard-2", boot_source = { type = "image", id = "img1" } }
    }
  }

  expect_failures = [var.instances]
}
