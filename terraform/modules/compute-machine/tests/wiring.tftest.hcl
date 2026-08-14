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
      boot_source_type        = "storage.image"
      boot_source_id          = "imgprobe00000000001"
      subnet_id               = "subprobe00000000001"
      security_group_ids      = ["sgprobe000000000001"]
      assign_external_address = true
    }
    db = {
      name                    = "db-1"
      machine_type_id         = "standard-v3-4-8"
      boot_source_type        = "registry.image"
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
    condition     = kacho_compute_instance.this["web"].boot_source.type == "storage.image"
    error_message = "источник загрузки первой машины не доехал"
  }

  assert {
    condition     = kacho_compute_instance.this["db"].boot_source.type == "registry.image"
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
        name                    = "web-1"
        machine_type_id         = "standard-v3-2-4"
        boot_source_type        = "ISO"
        boot_source_id          = "imgprobe00000000001"
        subnet_id               = "subprobe00000000001"
        acknowledge_unreachable = true
      }
    }
  }

  expect_failures = [var.machines]
}

# ── Ключи входа и группы размещения ──────────────────────────────────────────
#
# Проверяется РАЗРЕШЕНИЕ ссылок, а не факт создания ресурса: ссылка по ключу —
# единственное, ради чего оба ресурса живут в этом модуле, и единственное, что
# вызывающий не может проверить сам.

run "machine_takes_the_group_and_the_keys_the_module_created" {
  command = plan

  variables {
    guest_access_keys = {
      operator = { name = "operator", public_key = "ssh-ed25519 AAAAoperator" }
      backup   = { name = "backup", public_key = "ssh-ed25519 AAAAbackup" }
    }

    placement_groups = {
      spread = { name = "app-spread", strategy = "SPREAD", zone_id = "ru-central1-a" }
    }

    machines = {
      web = {
        name                  = "web-1"
        machine_type_id       = "standard-v3-2-4"
        boot_source_type      = "storage.image"
        boot_source_id        = "imgprobe00000000001"
        subnet_id             = "subprobe00000000001"
        placement_group_key   = "spread"
        guest_access_key_keys = ["operator", "backup"]
        # Достижимость названа: край требует одного из двух у машины рода VM, и
        # модуль этот выбор ПРОВОДИТ, а не делает за вызывающего. Проба, его не
        # называющая, падала бы на страже — то есть на чужом предмете.
        acknowledge_unreachable = true
      }
    }
  }

  assert {
    condition     = kacho_compute_placement_group.this["spread"].strategy == "SPREAD"
    error_message = "стратегия группы не доехала"
  }

  # Якорь: заданная координата доезжает, НЕзаданная не подставляется зоной набора.
  # Второе утверждение существеннее первого — оно ловит соблазн вывести регион из
  # зоны, из-за которого региональная группа стала бы невыразимой.
  assert {
    condition     = kacho_compute_placement_group.this["spread"].zone_id == "ru-central1-a"
    error_message = "координата якоря не доехала"
  }

  assert {
    condition     = kacho_compute_placement_group.this["spread"].region_id == null
    error_message = "модуль подставил регион, которого вызывающий не называл: региональная группа стала бы неотличима от зональной"
  }

  assert {
    condition     = kacho_compute_guest_access_key.this["operator"].public_key == "ssh-ed25519 AAAAoperator"
    error_message = "материал первого ключа не доехал"
  }

  # Два ключа в одном прогоне — так проверяется РАЗЛИЧЕНИЕ: один прошёл бы и при
  # жёстко зашитом значении.
  assert {
    condition     = kacho_compute_guest_access_key.this["backup"].public_key == "ssh-ed25519 AAAAbackup"
    error_message = "материал второго ключа не доехал — модуль подставляет один на всех"
  }

  assert {
    condition     = length(kacho_compute_instance.this["web"].guest_access_key_ids) == 2
    error_message = "машина получила не два ключа: ссылки по ключу карты не разрешились"
  }
}

run "external_and_module_keys_are_added_together_not_chosen_between" {
  command = plan

  variables {
    guest_access_keys = {
      operator = { name = "operator", public_key = "ssh-ed25519 AAAAoperator" }
    }

    machines = {
      web = {
        name                    = "web-1"
        machine_type_id         = "standard-v3-2-4"
        boot_source_type        = "storage.image"
        boot_source_id          = "imgprobe00000000001"
        subnet_id               = "subprobe00000000001"
        guest_access_key_keys   = ["operator"]
        guest_access_key_ids    = ["gak-outsider00000001"]
        acknowledge_unreachable = true
      }
    }
  }

  # Набор ключей — МНОЖЕСТВО, а не выбор: у машины законно бывают и свои, и чужие.
  # Утверждение на длине ловит реализацию, где один источник затирает другой.
  assert {
    condition     = length(kacho_compute_instance.this["web"].guest_access_key_ids) == 2
    error_message = "ключи модуля и внешние не сложились — один источник затёр другой"
  }

  assert {
    condition     = contains(kacho_compute_instance.this["web"].guest_access_key_ids, "gak-outsider00000001")
    error_message = "внешний ключ потерян"
  }
}

# Положительный контроль к отрицаниям ниже: без ключей и групп модуль работает
# как прежде. Иначе проверки входа зеленели бы на модуле, отвергающем всё.
run "machine_without_keys_or_groups_still_plans" {
  command = plan

  assert {
    condition     = length(kacho_compute_instance.this["web"].guest_access_key_ids) == 0
    error_message = "модуль навязал ключи машине, которой их не задавали"
  }
}

run "both_ways_to_name_a_group_at_once_are_rejected" {
  command = plan

  variables {
    placement_groups = {
      spread = { name = "app-spread", strategy = "SPREAD", zone_id = "ru-central1-a" }
    }

    machines = {
      web = {
        name                    = "web-1"
        machine_type_id         = "standard-v3-2-4"
        boot_source_type        = "storage.image"
        boot_source_id          = "imgprobe00000000001"
        subnet_id               = "subprobe00000000001"
        placement_group_key     = "spread"
        placement_group_id      = "plg-outsider00000001"
        acknowledge_unreachable = true
      }
    }
  }

  expect_failures = [var.machines]
}

run "both_anchor_coordinates_at_once_are_rejected" {
  command = plan

  variables {
    placement_groups = {
      bad = {
        name      = "bad"
        strategy  = "SPREAD"
        zone_id   = "ru-central1-a"
        region_id = "ru-central1"
      }
    }
  }

  expect_failures = [var.placement_groups]
}

run "no_anchor_coordinate_at_all_is_rejected" {
  command = plan

  variables {
    placement_groups = {
      bad = { name = "bad", strategy = "SPREAD" }
    }
  }

  expect_failures = [var.placement_groups]
}

run "unknown_strategy_is_rejected" {
  command = plan

  variables {
    placement_groups = {
      bad = { name = "bad", strategy = "SPREAD_2", zone_id = "ru-central1-a" }
    }
  }

  expect_failures = [var.placement_groups]
}

# Закрытая половина ключа отвергается ВХОДОМ модуля, а не только краем: к тому
# моменту, как её отверг бы край, она уже лежала бы в плане и в состоянии.
run "private_half_of_a_key_is_rejected_at_the_module_boundary" {
  command = plan

  variables {
    guest_access_keys = {
      operator = {
        name       = "operator"
        public_key = "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaA==\n-----END OPENSSH PRIVATE KEY-----"
      }
    }
  }

  expect_failures = [var.guest_access_keys]
}
