# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: проверяют СВЯЗЫВАНИЕ и проверки входа, не обращаясь к краю.
# Провайдер подменён mock_provider — падение означает ошибку в модуле, а не
# состояние стенда.
#
# Чего здесь НЕТ и почему: утверждений о необязательных полях, оставленных
# незаданными. Подменённый провайдер заполняет вычисляемые поля сгенерированным
# значением, а необязательное поле схемы вычисляемо, — такая проба зеленела бы и
# краснела по причинам, к модулю отношения не имеющим. Утверждается только то,
# что вызывающий ЗАДАЛ.

mock_provider "kacho" {}

variables {
  project_id = "prjprobe000000000000"
  zone_id    = "ru-central1-a"
  labels     = { suite = "wiring" }

  machines = {
    # Достижимость названа у ОБЕИХ машин и названа ПО-РАЗНОМУ: страж края требует
    # ровно одного из двух, и фикстура, выбравшая одно и то же дважды, проверяла бы
    # только половину прохода.
    web = {
      name                    = "web-1"
      machine_type_id         = "standard-v3-2-4"
      boot_source_type        = "IMAGE"
      boot_source_id          = "imgprobe00000000001"
      subnet_id               = "subprobe00000000001"
      security_group_ids      = ["sgprobe000000000001"]
      assign_external_address = true
    }
    db = {
      name                    = "db-1"
      machine_type_id         = "standard-v3-4-8"
      boot_source_type        = "SNAPSHOT"
      boot_source_id          = "snpprobe00000000001"
      subnet_id               = "subprobe00000000001"
      acknowledge_unreachable = true
    }
  }
}

run "each_machine_takes_the_set_zone_and_its_own_boot_source" {
  command = plan

  assert {
    condition     = kacho_compute_instance.this["web"].zone_id == "ru-central1-a"
    error_message = "зона набора обязана доезжать до машины: без неё когерентность с подсетью не выражена"
  }

  # Два источника в одном прогоне — так проверяется РАЗЛИЧЕНИЕ, а не то, что поле
  # вообще присваивается. Один источник прошёл бы и при жёстко зашитом значении.
  #
  # Читается ВЛОЖЕННЫМ блоком: у ресурса источник — один объект `boot_source{type,id}`,
  # а не пара плоских полей. Плоскую пару несла версия ресурса, снятая при сведении со
  # стволом как дубль; вход самого модуля остался плоским и здесь не меняется.
  assert {
    condition     = kacho_compute_instance.this["web"].boot_source.type == "IMAGE"
    error_message = "источник загрузки первой машины не доехал"
  }

  assert {
    condition     = kacho_compute_instance.this["db"].boot_source.type == "SNAPSHOT"
    error_message = "источник загрузки второй машины не доехал — модуль подставляет свой вместо заданного"
  }

  assert {
    condition     = kacho_compute_instance.this["db"].boot_source.id == "snpprobe00000000001"
    error_message = "идентификатор источника не доехал"
  }

  # Метки объявлены на уровне набора и обязаны попасть КАЖДОЙ машине: проба на
  # одной прошла бы и при потере второй.
  assert {
    condition     = kacho_compute_instance.this["web"].labels["suite"] == "wiring"
    error_message = "метки набора не доехали до первой машины"
  }

  assert {
    condition     = kacho_compute_instance.this["db"].labels["suite"] == "wiring"
    error_message = "метки набора не доехали до второй машины"
  }
}

run "unknown_boot_source_is_rejected_by_input_validation" {
  command = plan

  variables {
    machines = {
      web = {
        name             = "web-1"
        machine_type_id  = "standard-v3-2-4"
        boot_source_type = "ISO"
        boot_source_id   = "imgprobe00000000001"
        subnet_id        = "subprobe00000000001"
      }
    }
  }

  expect_failures = [var.machines]
}
