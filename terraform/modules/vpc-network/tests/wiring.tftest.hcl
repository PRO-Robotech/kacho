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

# ---- группа безопасности по умолчанию ----------------------------------------------------

# Здесь стояли две пробы условного создания группы по умолчанию — снято вместе с предметом.
#
# Они проверяли, что решение о группе принимает конфигурация, а не настройка развёртывания
# края, и были верны для того контракта. Поле, которым группу можно было отменить, снято с
# резервированием номера и имени: группа создаётся БЕЗУСЛОВНО, потому что интерфейс её
# наследует, а сеть без группы означала бы интерфейс без единого правила — по закрытой
# модели «не разрешено ничего».
#
# Безусловность закреплена на уровне сервиса (проба конструктора, который признак условности
# БОЛЬШЕ НЕ ПРИНИМАЕТ: появись он снова, вызов перестанет компилироваться) — это строже
# утверждения, «не собралось» нельзя обойти толерантностью. Здесь проверять нечего: у
# модуля больше нет ручки, о которой шла речь.

# ---- шлюз общего исхода -----------------------------------------------------------------

# Умолчание — сети без шлюза: вызывающий, ничего не менявший, не должен получить новый
# ресурс от обновления модуля. Положительный контроль к пробе ниже: без него «шлюза нет»
# зеленело бы и на модуле, который его никогда не создаёт.
run "gateway_is_absent_by_default" {
  command = plan

  assert {
    condition     = length(kacho_vpc_gateway.this) == 0
    error_message = "шлюз завёлся без просьбы — обновление модуля добавило ресурс тем, кто ничего не менял"
  }
}

run "gateway_is_created_when_asked" {
  command = plan

  variables {
    create_gateway = true
    gateway_kind   = "nat"
    gateway_subnet = "probe-a"
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
  }

  assert {
    condition     = length(kacho_vpc_gateway.this) == 1
    error_message = "флаг включён, а шлюза нет"
  }
  # Имя выведено из имени сети: флаг включается одной строкой и не тянет за собой
  # обязательную переменную.
  assert {
    condition     = one(kacho_vpc_gateway.this[*].name) == "probe-net-gw"
    error_message = "имя шлюза не выведено из имени сети"
  }
  assert {
    condition     = one(kacho_vpc_gateway.this[*].project_id) == var.project_id
    error_message = "шлюз заведён не в проекте модуля"
  }
}

run "explicit_gateway_name_wins_over_derived" {
  command = plan

  variables {
    create_gateway = true
    gateway_name   = "probe-egress"
    gateway_kind   = "nat"
    gateway_subnet = "probe-a"
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
  }

  assert {
    condition     = one(kacho_vpc_gateway.this[*].name) == "probe-egress"
    error_message = "явное имя шлюза подменено выведенным"
  }
}

# Метки шлюза — СВОЯ переменная: `labels` объявлена метками сети, и распространение её на
# новый ресурс сменило бы смысл переменной у тех, кто модуль уже вызывает.
run "gateway_labels_are_separate_from_network_labels" {
  command = plan

  variables {
    create_gateway = true
    labels         = { owner = "net" }
    gateway_labels = { owner = "gw" }
    gateway_kind   = "nat"
    gateway_subnet = "probe-a"
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
  }

  assert {
    condition     = one(kacho_vpc_gateway.this[*].labels)["owner"] == "gw"
    error_message = "метки шлюза взяты не из своей переменной"
  }
  assert {
    condition     = kacho_vpc_network.this.labels["owner"] == "net"
    error_message = "метки сети изменились от появления шлюза"
  }
}

# ---- сетевые интерфейсы -----------------------------------------------------------------

# Интерфейс ссылается на подсеть ПО ИДЕНТИФИКАТОРУ, взятому по ключу карты: ссылка строит
# граф, и снос идёт в обратном порядке — интерфейсы раньше подсети.
run "interface_references_the_subnet_by_id_from_map_key" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
      "probe-b" = { zone_id = "ru-central1-b", ipv4_cidr_primary = "10.90.2.0/24" }
    }
    network_interfaces = {
      "probe-nic" = { subnet = "probe-b" }
    }
  }

  assert {
    condition     = kacho_vpc_network_interface.this["probe-nic"].subnet_id == kacho_vpc_subnet.this["probe-b"].id
    error_message = "интерфейс связан не с той подсетью, которую назвал ключ"
  }
  # Ключ карты — имя интерфейса, как и у подсетей: одна карта, одно правило.
  assert {
    condition     = kacho_vpc_network_interface.this["probe-nic"].name == "probe-nic"
    error_message = "имя интерфейса берётся не из ключа карты"
  }
  assert {
    condition     = kacho_vpc_network_interface.this["probe-nic"].project_id == var.project_id
    error_message = "интерфейс заведён не в проекте модуля"
  }
}

# Значения собираются полем за полем; проба закрепляет, что сборка ничего не теряет.
run "interface_fields_survive_assembly" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    network_interfaces = {
      "probe-nic" = {
        subnet             = "probe-a"
        description        = "интерфейс пробы"
        labels             = { role = "probe" }
        security_group_ids = ["sgpprobe0000000000000"]
        v4_address_ids     = ["adrprobe0000000000000"]
      }
    }
  }

  assert {
    condition     = one(kacho_vpc_network_interface.this["probe-nic"].security_group_ids) == "sgpprobe0000000000000"
    error_message = "группа безопасности потеряна при сборке"
  }
  assert {
    condition     = one(kacho_vpc_network_interface.this["probe-nic"].v4_address_ids) == "adrprobe0000000000000"
    error_message = "адрес интерфейса потерян при сборке"
  }
  assert {
    condition     = kacho_vpc_network_interface.this["probe-nic"].labels["role"] == "probe"
    error_message = "метки интерфейса потеряны при сборке"
  }
}

# Умолчание групп безопасности — пустой список, и это «ни одной группы»: край на создании
# ничего не наследует. Проба закрепляет умолчание, а не догадку о нём.
run "interface_security_groups_default_to_none" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    network_interfaces = {
      "probe-nic" = { subnet = "probe-a" }
    }
  }

  assert {
    condition     = length(kacho_vpc_network_interface.this["probe-nic"].security_group_ids) == 0
    error_message = "модуль подставил группу безопасности, о которой вызывающий не просил"
  }
}

# ОТРИЦАНИЯ на новое. Без них положительные пробы выше зеленели бы и на модуле, который
# принимает что угодно.

# Ключ, которого нет среди подсетей, обязан отвергаться проверкой входа с внятным текстом,
# а не падать индексацией карты ресурсов: «Invalid index» не называет ни причины, ни того,
# что вызывающий обязан исправить.
run "interface_pointing_at_unknown_subnet_key_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    network_interfaces = {
      "probe-nic" = { subnet = "probe-missing" }
    }
  }

  expect_failures = [var.network_interfaces]
}

# Идентификатор подсети вместо ключа — тоже промах: он разорвал бы ребро графа, ради
# которого ключ и введён.
run "interface_pointing_at_subnet_id_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    network_interfaces = {
      "probe-nic" = { subnet = "subprobe0000000000000" }
    }
  }

  expect_failures = [var.network_interfaces]
}

# Второй адрес одного семейства край отвергает: несколько адресов на машине набираются
# несколькими интерфейсами.
run "interface_with_two_v4_addresses_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    network_interfaces = {
      "probe-nic" = {
        subnet         = "probe-a"
        v4_address_ids = ["adrprobe0000000000000", "adrprobe0000000000001"]
      }
    }
  }

  expect_failures = [var.network_interfaces]
}

# То же ограничение для второго семейства. Отдельная проба, а не довесок к предыдущей:
# условие проверки — конъюнкция двух половин (`v4 <= 1 && v6 <= 1`), и отрицание на одной
# половине оставляет вторую БЕЗ производителя отказа. Проверено инъекцией: со снятым
# конъюнктом про v6 весь набор оставался зелёным.
run "interface_with_two_v6_addresses_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    network_interfaces = {
      "probe-nic" = {
        subnet         = "probe-a"
        v6_address_ids = ["adrprobe0000000000000", "adrprobe0000000000001"]
      }
    }
  }

  expect_failures = [var.network_interfaces]
}

run "interface_with_empty_reference_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    network_interfaces = {
      "probe-nic" = { subnet = "probe-a", security_group_ids = [""] }
    }
  }

  expect_failures = [var.network_interfaces]
}

run "gateway_name_with_uppercase_is_rejected" {
  command = plan

  variables {
    create_gateway = true
    gateway_name   = "Probe-GW"
  }

  expect_failures = [var.gateway_name]
}

# Имя сети с заглавной буквой законно — до тех пор, пока из него не выводят имя шлюза.
# Поэтому отказ приходит от предусловия ресурса, а не от переменной `name`: пока шлюз не
# заводят, эта же сеть обязана планироваться без единой жалобы.
run "derived_gateway_name_inheriting_uppercase_is_rejected" {
  command = plan

  variables {
    name           = "Probe_Net"
    create_gateway = true
  }

  expect_failures = [kacho_vpc_gateway.this]
}

# Парный положительный к пробе выше: то же имя сети без шлюза проходит. Без него отказ выше
# был бы неотличим от «модуль вообще не принимает заглавных в имени сети».
run "uppercase_network_name_is_legal_without_a_gateway" {
  command = plan

  variables {
    name = "Probe_Net"
  }

  assert {
    condition     = kacho_vpc_network.this.name == "Probe_Net"
    error_message = "имя сети с заглавной буквой отвергнуто, хотя край его принимает"
  }
}

# ---- группы безопасности -----------------------------------------------------------------

# Группа заводится В СЕТИ модуля и ссылается на неё по идентификатору: снос идёт в обратном
# порядке — группы раньше сети.
run "security_group_is_created_in_the_network" {
  command = plan

  variables {
    security_groups = {
      "probe-web" = {
        rules = [{
          direction      = "INGRESS"
          protocol_name  = "tcp"
          ports          = { from_port = 443, to_port = 443 }
          v4_cidr_blocks = ["0.0.0.0/0"]
        }]
      }
    }
  }

  assert {
    condition     = kacho_vpc_security_group.this["probe-web"].network_id == kacho_vpc_network.this.id
    error_message = "группа безопасности не связана с сетью модуля"
  }
  assert {
    condition     = kacho_vpc_security_group.this["probe-web"].name == "probe-web"
    error_message = "имя группы берётся не из ключа карты"
  }
  assert {
    condition     = kacho_vpc_security_group.this["probe-web"].project_id == var.project_id
    error_message = "группа заведена не в проекте модуля"
  }
}

# Значения правила собираются полем за полем; проба закрепляет, что сборка ничего не теряет
# И что необязательные части собираются ТОЛЬКО когда заданы. Две группы в одном прогоне: у
# одной диапазон портов и протокол именем, у другой ни того ни другого — иначе утверждение
# «опущенный ports означает любой порт» зеленело бы и на модуле, который ports вообще не
# умеет передавать.
run "security_group_rule_fields_survive_assembly" {
  command = plan

  variables {
    security_groups = {
      "probe-web" = {
        rules = [{
          direction      = "INGRESS"
          description    = "правило пробы"
          labels         = { role = "probe" }
          protocol_name  = "tcp"
          ports          = { from_port = 80, to_port = 8080 }
          v4_cidr_blocks = ["10.90.0.0/16"]
          v6_cidr_blocks = ["2001:db8::/32"]
        }]
      }
      "probe-egress" = {
        rules = [{
          direction       = "EGRESS"
          protocol_number = 47
          # Цель — ДРУГОГО рода, и это несущий выбор пробы, а не заполнение обязательного
          # поля. Утверждение ниже проверяет, что модуль НЕ подставляет блоки адресов,
          # о которых не просили; на цели-блоках его нечем было бы проверить. Прежде здесь
          # цели не было вовсе — до того, как правило обязали называть ровно одну.
          security_group_id = "sgpprobe0000000000000"
        }]
      }
    }
  }

  assert {
    condition     = one(kacho_vpc_security_group.this["probe-web"].rules).direction == "INGRESS"
    error_message = "направление правила потеряно при сборке"
  }
  assert {
    condition = (
      one(kacho_vpc_security_group.this["probe-web"].rules).ports.from_port == 80 &&
      one(kacho_vpc_security_group.this["probe-web"].rules).ports.to_port == 8080
    )
    error_message = "диапазон портов потерян при сборке"
  }
  assert {
    condition     = one(kacho_vpc_security_group.this["probe-web"].rules).protocol_name == "tcp"
    error_message = "протокол именем потерян при сборке"
  }
  assert {
    condition = (
      one(one(kacho_vpc_security_group.this["probe-web"].rules).cidr_blocks.v4_cidr_blocks) == "10.90.0.0/16" &&
      one(one(kacho_vpc_security_group.this["probe-web"].rules).cidr_blocks.v6_cidr_blocks) == "2001:db8::/32"
    )
    error_message = "блоки адресов цели потеряны при сборке"
  }
  assert {
    condition = (
      one(kacho_vpc_security_group.this["probe-web"].rules).description == "правило пробы" &&
      one(kacho_vpc_security_group.this["probe-web"].rules).labels["role"] == "probe"
    )
    error_message = "описание или метки правила потеряны при сборке"
  }
  # Опущенный диапазон означает у края ЛЮБОЙ порт, и модуль обязан передать именно опускание:
  # подставленное умолчание сказало бы краю не то, о чём просили.
  assert {
    condition     = one(kacho_vpc_security_group.this["probe-egress"].rules).ports == null
    error_message = "модуль подставил диапазон портов, о котором вызывающий не просил"
  }
  assert {
    condition = (
      one(kacho_vpc_security_group.this["probe-egress"].rules).protocol_number == 47 &&
      one(kacho_vpc_security_group.this["probe-egress"].rules).protocol_name == null
    )
    error_message = "протокол номером потерян при сборке либо подменён именем"
  }
  # Цель — предопределённое имя: объект блоков не собирается вовсе, иначе край получил бы
  # цель `cidr_blocks` без единого блока и сохранил правило без цели.
  assert {
    condition = (
      one(kacho_vpc_security_group.this["probe-egress"].rules).cidr_blocks == null
    )
    error_message = "цель правила подменена при сборке"
  }
}

# Группа БЕЗ правил не пропускает ничего — это её смысл. Проба закрепляет, что модуль не
# подставляет правил от себя: правило «любой протокол отовсюду», добавленное умолчанием,
# открыло бы интерфейс, о котором никто не просил.
run "security_group_without_rules_stays_closed" {
  command = plan

  variables {
    security_groups = {
      "probe-locked" = {}
    }
  }

  assert {
    condition     = length(kacho_vpc_security_group.this["probe-locked"].rules) == 0
    error_message = "модуль подставил правило в группу, заявленную пустой"
  }
}

# ОТРИЦАНИЯ на проверки входа групп. Без них положительные пробы выше зеленели бы и на
# модуле, который принимает что угодно.

# Правило без цели край принимает МОЛЧА и отдаёт обратно без цели — то есть как другое
# правило, чем написал вызывающий. Отказ обязан прийти на проверке входа.
run "security_group_rule_without_a_target_is_rejected" {
  command = plan

  variables {
    security_groups = {
      "probe-web" = {
        rules = [{ direction = "INGRESS", protocol_name = "tcp" }]
      }
    }
  }

  expect_failures = [var.security_groups]
}

run "security_group_rule_with_two_targets_is_rejected" {
  command = plan

  variables {
    security_groups = {
      "probe-web" = {
        rules = [{
          direction         = "INGRESS"
          v4_cidr_blocks    = ["0.0.0.0/0"]
          security_group_id = "sgrprobe0000000000000"
        }]
      }
    }
  }

  expect_failures = [var.security_groups]
}

# Протокол — ОДНО поле с двумя написаниями: выбрать за вызывающего, какое он имел в виду,
# модуль не вправе.
run "security_group_rule_with_protocol_named_and_numbered_is_rejected" {
  command = plan

  variables {
    security_groups = {
      "probe-web" = {
        rules = [{
          direction       = "INGRESS"
          protocol_name   = "tcp"
          protocol_number = 6
          v4_cidr_blocks  = ["0.0.0.0/0"]
        }]
      }
    }
  }

  expect_failures = [var.security_groups]
}

# Пустая строка — не значение цели: край читает её как «цель не задана» и сохраняет правило
# ШИРЕ написанного, ничем не пожаловавшись.
run "security_group_rule_with_empty_target_id_is_rejected" {
  command = plan

  variables {
    security_groups = {
      "probe-web" = {
        rules = [{ direction = "INGRESS", security_group_id = "" }]
      }
    }
  }

  expect_failures = [var.security_groups]
}

# Здесь стоял остаток пробы предопределённой цели — снято вместе с предметом.
#
# Ветвь снята с контракта: свободная строка без словаря, которую край принимал и не читал.
# «Ровно одна цель» проверяется соседними пробами и энфорсером на стороне сервиса.

# ---- таблицы маршрутизации ----------------------------------------------------------------

run "route_table_is_created_in_the_network" {
  command = plan

  variables {
    route_tables = {
      "probe-main" = {
        routes = [{ destination_prefix = "0.0.0.0/0", next_hop_address = "10.90.0.1" }]
      }
    }
  }

  assert {
    condition     = kacho_vpc_route_table.this["probe-main"].network_id == kacho_vpc_network.this.id
    error_message = "таблица маршрутизации не связана с сетью модуля"
  }
  assert {
    condition     = kacho_vpc_route_table.this["probe-main"].name == "probe-main"
    error_message = "имя таблицы берётся не из ключа карты"
  }
  assert {
    condition     = kacho_vpc_route_table.this["probe-main"].project_id == var.project_id
    error_message = "таблица заведена не в проекте модуля"
  }
}

# Маршруты — список, и порядок сохраняется дословно: край адресует свои отказы индексом, и у
# списка этот индекс совпадает с индексом в конфигурации. Проба утверждает ОБА индекса —
# утверждение об одном нулевом зеленело бы и на перестановке.
run "route_table_routes_keep_their_order_and_fields" {
  command = plan

  variables {
    route_tables = {
      "probe-main" = {
        routes = [
          {
            destination_prefix = "0.0.0.0/0"
            next_hop_address   = "10.90.0.1"
            labels             = { role = "probe" }
          },
          {
            destination_prefix = "192.168.0.0/16"
            next_hop_address   = "10.90.0.2"
          },
        ]
      }
    }
  }

  assert {
    condition = (
      kacho_vpc_route_table.this["probe-main"].static_routes[0].destination_prefix == "0.0.0.0/0" &&
      kacho_vpc_route_table.this["probe-main"].static_routes[0].next_hop_address == "10.90.0.1"
    )
    error_message = "первый маршрут потерян или переставлен при сборке"
  }
  assert {
    condition = (
      kacho_vpc_route_table.this["probe-main"].static_routes[1].destination_prefix == "192.168.0.0/16" &&
      kacho_vpc_route_table.this["probe-main"].static_routes[1].next_hop_address == "10.90.0.2"
    )
    error_message = "второй маршрут потерян или переставлен при сборке"
  }
  assert {
    condition     = kacho_vpc_route_table.this["probe-main"].static_routes[0].labels["role"] == "probe"
    error_message = "метки маршрута потеряны при сборке"
  }
}

# Подсеть называет таблицу КЛЮЧОМ, а модуль подставляет идентификатор — ссылка строит граф,
# и подсеть снимается раньше таблицы. Рядом вторая подсеть с готовым чужим идентификатором:
# без неё проба зеленела бы и на модуле, который в route_table_id всегда подставляет СВОЮ
# таблицу, игнорируя написанное.
run "subnet_references_the_route_table_by_id_from_map_key" {
  command = plan

  variables {
    route_tables = {
      "probe-main" = {
        routes = [{ destination_prefix = "0.0.0.0/0", next_hop_address = "10.90.0.1" }]
      }
    }
    subnets = {
      "probe-a" = {
        zone_id           = "ru-central1-a"
        ipv4_cidr_primary = "10.90.1.0/24"
        route_table       = "probe-main"
      }
      "probe-b" = {
        zone_id           = "ru-central1-b"
        ipv4_cidr_primary = "10.90.2.0/24"
        route_table_id    = "rtbprobe0000000000000"
      }
    }
  }

  assert {
    condition     = kacho_vpc_subnet.this["probe-a"].route_table_id == kacho_vpc_route_table.this["probe-main"].id
    error_message = "подсеть не связана с таблицей маршрутизации модуля по идентификатору"
  }
  assert {
    condition     = kacho_vpc_subnet.this["probe-b"].route_table_id == "rtbprobe0000000000000"
    error_message = "готовый идентификатор чужой таблицы подменён таблицей модуля"
  }
}

run "subnet_pointing_at_unknown_route_table_key_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-a" = {
        zone_id           = "ru-central1-a"
        ipv4_cidr_primary = "10.90.1.0/24"
        route_table       = "probe-missing"
      }
    }
  }

  expect_failures = [var.subnets]
}

# Два написания одного поля сразу — не исход: выбрать за вызывающего, какое он имел в виду,
# модуль не вправе.
run "subnet_naming_the_route_table_twice_is_rejected" {
  command = plan

  variables {
    route_tables = {
      "probe-main" = {
        routes = [{ destination_prefix = "0.0.0.0/0", next_hop_address = "10.90.0.1" }]
      }
    }
    subnets = {
      "probe-a" = {
        zone_id           = "ru-central1-a"
        ipv4_cidr_primary = "10.90.1.0/24"
        route_table       = "probe-main"
        route_table_id    = "rtbprobe0000000000000"
      }
    }
  }

  expect_failures = [var.subnets]
}

# ---- адреса ---------------------------------------------------------------------------------

# Внутренний адрес называет подсеть КЛЮЧОМ, а модуль подставляет идентификатор: ссылка строит
# граф, и адрес снимается раньше подсети, из которой выдан. Две подсети в прогоне — чтобы
# утверждение говорило о ТОЙ подсети, которую назвал ключ, а не о единственной имеющейся.
run "internal_address_references_the_subnet_by_id_from_map_key" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
      "probe-b" = { zone_id = "ru-central1-b", ipv4_cidr_primary = "10.90.2.0/24" }
    }
    addresses = {
      "probe-db" = { internal_ipv4 = { subnet = "probe-b", address = "10.90.2.7" } }
    }
  }

  assert {
    condition     = kacho_vpc_address.internal_ipv4["probe-db"].internal_ipv4.subnet_id == kacho_vpc_subnet.this["probe-b"].id
    error_message = "адрес выдан не из той подсети, которую назвал ключ"
  }
  assert {
    condition     = kacho_vpc_address.internal_ipv4["probe-db"].internal_ipv4.address == "10.90.2.7"
    error_message = "заданный адрес потерян при сборке"
  }
  assert {
    condition     = kacho_vpc_address.internal_ipv4["probe-db"].name == "probe-db"
    error_message = "имя адреса берётся не из ключа карты"
  }
  assert {
    condition     = kacho_vpc_address.internal_ipv4["probe-db"].project_id == var.project_id
    error_message = "адрес заведён не в проекте модуля"
  }
}

# Внешний адрес подсети не несёт вовсе, и пустая зона у него законна. Проба утверждает
# ЗАДАННЫЕ значения: спецификация собирается полем за полем, и требования собираются только
# когда их заказали.
run "external_address_fields_survive_assembly" {
  command = plan

  variables {
    addresses = {
      "probe-vip" = {
        external_ipv4 = {
          address = "203.0.113.7"
          zone_id = "ru-central1-a"
        }
      }
      "probe-vip-global" = {
        external_ipv4 = {}
      }
    }
  }

  assert {
    condition     = kacho_vpc_address.external_ipv4["probe-vip"].external_ipv4.address == "203.0.113.7"
    error_message = "адрес внешней спецификации потерян при сборке"
  }
  assert {
    condition     = kacho_vpc_address.external_ipv4["probe-vip"].external_ipv4.zone_id == "ru-central1-a"
    error_message = "зона внешнего адреса потеряна при сборке"
  }
  # Пустая зона законна — это адрес из глобального пула. Парный контроль к утверждению выше:
  # без него «зона доехала» зеленело бы и на модуле, который подставляет зону сам.
  assert {
    condition     = kacho_vpc_address.external_ipv4["probe-vip-global"].external_ipv4.zone_id == null
    error_message = "модуль подставил зону адресу, для которого её не называли"
  }
}

# Вид адреса решает, В КАКОМ блоке он окажется: у одного блока спецификация названа
# безусловно, поэтому «ровно один вид» выполняется при любом состоянии карты. Проба
# закрепляет сам отбор — адреса разных видов не должны сваливаться в один блок и не должны
# пропадать.
run "addresses_land_in_the_block_of_their_kind" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    addresses = {
      "probe-db"  = { internal_ipv4 = { subnet = "probe-a" } }
      "probe-vip" = { external_ipv4 = {} }
    }
  }

  assert {
    condition = (
      length(kacho_vpc_address.internal_ipv4) == 1 &&
      length(kacho_vpc_address.external_ipv4) == 1
    )
    error_message = "адрес не попал в блок своего вида"
  }
  assert {
    condition = (
      length(kacho_vpc_address.internal_ipv6) == 0 &&
      length(kacho_vpc_address.external_ipv6) == 0
    )
    error_message = "адрес попал в блок чужого вида"
  }
  # Разложенное по четырём блокам собирается обратно в один выход. Утверждение о ВЫХОДЕ, а не
  # о блоках: потерять адрес при сборке — отдельная от отбора беда, и по числу экземпляров в
  # блоках она не видна.
  assert {
    condition     = length(output.address_ids) == 2
    error_message = "сборка выхода потеряла адрес одного из видов"
  }
  assert {
    condition = (
      output.address_ids["probe-db"] == kacho_vpc_address.internal_ipv4["probe-db"].id &&
      output.address_ids["probe-vip"] == kacho_vpc_address.external_ipv4["probe-vip"].id
    )
    error_message = "выход называет чужой идентификатор — ключи блоков разъехались при сборке"
  }
}

# ОТРИЦАНИЯ на проверки входа адресов.

run "address_without_a_kind_is_rejected" {
  command = plan

  variables {
    addresses = {
      "probe-nowhere" = { description = "адрес без вида" }
    }
  }

  expect_failures = [var.addresses]
}

run "address_with_two_kinds_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    addresses = {
      "probe-both" = {
        external_ipv4 = {}
        internal_ipv4 = { subnet = "probe-a" }
      }
    }
  }

  expect_failures = [var.addresses]
}

# Ключ, которого нет среди подсетей, обязан отвергаться проверкой входа с внятным текстом, а
# не падать индексацией карты ресурсов: «Invalid index» не называет ни карты, ни известных
# ключей.
run "internal_address_pointing_at_unknown_subnet_key_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    addresses = {
      "probe-db" = { internal_ipv4 = { subnet = "probe-missing" } }
    }
  }

  expect_failures = [var.addresses]
}

# Тот же промах во второй спецификации. Проверка собирает ключи ОБЕИХ внутренних веток в
# один список, поэтому отрицание на одной ветке оставляет вторую без производителя отказа.
# Проверено инъекцией: со снятой веткой internal_ipv6 весь набор оставался зелёным.
run "internal_ipv6_address_pointing_at_unknown_subnet_key_is_rejected" {
  command = plan

  variables {
    subnets = {
      "probe-a" = { zone_id = "ru-central1-a", ipv4_cidr_primary = "10.90.1.0/24" }
    }
    addresses = {
      "probe-db6" = { internal_ipv6 = { subnet = "probe-missing" } }
    }
  }

  expect_failures = [var.addresses]
}

# ---- именованные наборы префиксов ---------------------------------------------------------

# Правило ссылается на набор ПО ИДЕНТИФИКАТОРУ, взятому по ключу карты: ссылка строит граф,
# и снос идёт в обратном порядке — правило раньше набора. Порядок несущий: край не удаляет
# набор, на который ссылается живое правило.
run "rule_references_the_cidr_group_by_id_from_map_key" {
  command = plan

  variables {
    cidr_groups = {
      "probe-office" = { v4_cidr_blocks = ["203.0.113.0/24"] }
    }
    security_groups = {
      "probe-sg" = {
        rules = [{
          direction  = "INGRESS"
          cidr_group = "probe-office"
        }]
      }
    }
  }

  assert {
    condition = one([
      for r in kacho_vpc_security_group.this["probe-sg"].rules : r.cidr_group_id
    ]) == kacho_vpc_cidr_group.this["probe-office"].id
    error_message = "правило не связано с набором модуля — снос пошёл бы в произвольном порядке"
  }
  assert {
    condition     = kacho_vpc_cidr_group.this["probe-office"].name == "probe-office"
    error_message = "имя набора берётся не из ключа карты"
  }
  assert {
    condition     = kacho_vpc_cidr_group.this["probe-office"].project_id == var.project_id
    error_message = "набор заведён не в проекте модуля"
  }
}

# Готовый идентификатор набора, заведённого ВНЕ модуля, доезжает как есть. Парный
# положительный контроль к пробе выше: без него «ключ разрешается» зеленело бы и на модуле,
# который второе написание молча выбрасывает.
run "rule_accepts_an_external_cidr_group_id" {
  command = plan

  variables {
    security_groups = {
      "probe-sg" = {
        rules = [{
          direction     = "INGRESS"
          cidr_group_id = "cdg-0123456789abcdefg"
        }]
      }
    }
  }

  assert {
    condition = one([
      for r in kacho_vpc_security_group.this["probe-sg"].rules : r.cidr_group_id
    ]) == "cdg-0123456789abcdefg"
    error_message = "готовый идентификатор набора до края не доехал"
  }
}

# ОТРИЦАНИЯ. Без них положительные пробы выше зеленели бы и на модуле, принимающем что
# угодно: проверки входа существуют ровно затем, чтобы отвергать.

# Два написания одной цели сразу — выбрать за вызывающего модуль не вправе.
run "rule_with_both_cidr_group_spellings_is_rejected" {
  command = plan

  variables {
    cidr_groups = {
      "probe-office" = { v4_cidr_blocks = ["203.0.113.0/24"] }
    }
    security_groups = {
      "probe-sg" = {
        rules = [{
          direction     = "INGRESS"
          cidr_group    = "probe-office"
          cidr_group_id = "cdg-0123456789abcdefg"
        }]
      }
    }
  }

  expect_failures = [var.security_groups]
}

# Ключ, которого нет среди наборов, обязан отвергаться проверкой входа с внятным текстом, а
# не падать индексацией карты ресурсов: «Invalid index» не называет ни карты, ни ключей.
run "rule_pointing_at_unknown_cidr_group_key_is_rejected" {
  command = plan

  variables {
    security_groups = {
      "probe-sg" = {
        rules = [{
          direction  = "INGRESS"
          cidr_group = "probe-missing"
        }]
      }
    }
  }

  expect_failures = [var.security_groups]
}

# Пустой набор целью быть не может: край отвергает такое правило, и проверка ловит это
# раньше обращения к нему.
run "rule_pointing_at_an_empty_cidr_group_is_rejected" {
  command = plan

  variables {
    cidr_groups = {
      "probe-empty" = { description = "набор без единого члена" }
    }
    security_groups = {
      "probe-sg" = {
        rules = [{
          direction  = "INGRESS"
          cidr_group = "probe-empty"
        }]
      }
    }
  }

  expect_failures = [var.security_groups]
}

# Две цели сразу (набор и другая группа) — по-прежнему отказ: «ровно одна» теперь считает
# три ветви, и без этой пробы третья могла бы не попасть в счёт молча.
run "rule_with_cidr_group_and_security_group_is_rejected" {
  command = plan

  variables {
    cidr_groups = {
      "probe-office" = { v4_cidr_blocks = ["203.0.113.0/24"] }
    }
    security_groups = {
      "probe-sg" = {
        rules = [{
          direction         = "INGRESS"
          cidr_group        = "probe-office"
          security_group_id = "sgr01234567890abcde"
        }]
      }
    }
  }

  expect_failures = [var.security_groups]
}
