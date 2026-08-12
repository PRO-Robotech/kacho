# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: проверяют СВЯЗЫВАНИЕ и проверки входа, не обращаясь к краю.
# Провайдер подменён mock_provider — падение означает ошибку в модуле, а не состояние стенда.

mock_provider "kacho" {}

variables {
  project_id       = "prjprobe000000000000"
  name             = "probe-net"
  ipv4_cidr_blocks = ["10.90.0.0/16"]
}

# Подсеть ссылается на сеть ПО ИДЕНТИФИКАТОРУ, а не по имени: ссылка строит граф
# зависимостей, и снос идёт в обратном порядке — подсети раньше сети.
run "subnet_references_the_network_by_id" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
  }

  assert {
    condition     = kacho_vpc_subnet.this["probe-a"].network_id == kacho_vpc_network.this.id
    error_message = "подсеть не связана с сетью модуля"
  }
  assert {
    condition     = kacho_vpc_subnet.this["probe-a"].name == "probe-a"
    error_message = "имя подсети берётся не из ключа карты"
  }
}

# Региональная (anycast) подсеть зоны не несёт вовсе — и это законно.
run "regional_subnet_carries_no_zone" {
  command = plan

  variables {
    subnets = {
      "probe-any" = { region_id = "ru-central1", ipv4_cidr_primary = "10.90.2.0/24" }
    }
  }

  assert {
    condition     = kacho_vpc_subnet.this["probe-any"].region_id == "ru-central1"
    error_message = "регион подсети не доехал"
  }
}

# ОТРИЦАНИЕ. Без него положительные пробы выше зеленели бы и на модуле, который принимает
# что угодно: проверка входа существует ровно затем, чтобы отвергать.
run "subnet_with_both_anchors_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-bad" = {
        zone_id           = "ru-central1-a"
        region_id         = "ru-central1"
        ipv4_cidr_primary = "10.90.3.0/24"
      }
    }
  }

  expect_failures = [var.subnets]
}

run "subnet_without_any_anchor_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-bad" = { ipv4_cidr_primary = "10.90.4.0/24" }
    }
  }

  expect_failures = [var.subnets]
}

# Сеть без объявленного супернета подсеть НЕ примет, поэтому модуль требует его непустым.
run "empty_supernet_is_rejected" {
  command = plan

  variables {
    ipv4_cidr_blocks = []
  }

  expect_failures = [var.ipv4_cidr_blocks]
}
