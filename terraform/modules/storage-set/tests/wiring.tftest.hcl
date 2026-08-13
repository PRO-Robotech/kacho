# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: проверяют СВЯЗЫВАНИЕ и проверки входа, не обращаясь к краю.
# Провайдер подменён mock_provider — падение означает ошибку в модуле, а не состояние стенда.
#
# Чего здесь НЕТ и почему: утверждений вида «незаданный источник остался null». Подменённый
# провайдер заполняет вычисляемые поля сгенерированным значением, а необязательное поле
# схемы вычисляемо; такая проба зеленела бы или краснела по причинам, к модулю отношения
# не имеющим. Различение источников закреплено иначе — двумя образами в одном прогоне,
# у каждого свой источник, и оба утверждения о ЗАДАННЫХ значениях.

mock_provider "kacho" {}

variables {
  project_id = "prjprobe000000000000"
  region_id  = "ru-central1"

  volumes = {
    data = { zone_id = "ru-central1-a", size_bytes = 10737418240, disk_type_id = "network-ssd" }
  }
}

# Снимок ссылается на том ПО ИДЕНТИФИКАТОРУ, хотя вызывающий назвал ключ карты: ссылка
# строит граф, и снос идёт в обратном порядке — снимок раньше тома, с которого снят.
run "snapshot_references_its_volume_by_id" {
  command = plan

  variables {
    snapshots = { nightly = { source_volume_key = "data" } }
  }

  assert {
    condition     = kacho_storage_snapshot.this["nightly"].source_volume_id == kacho_storage_volume.this["data"].id
    error_message = "снимок не связан с томом модуля по идентификатору"
  }
  assert {
    condition     = kacho_storage_snapshot.this["nightly"].name == "nightly"
    error_message = "имя снимка не взято из ключа карты"
  }
}

# Образ из снимка и образ из тома в одном прогоне: у каждого свой источник, и оба
# утверждения — о заданных значениях, а не об отсутствии.
run "image_source_resolves_to_the_right_id" {
  command = plan

  variables {
    snapshots = { nightly = { source_volume_key = "data" } }
    images = {
      from_snapshot = { source_snapshot_key = "nightly" }
      from_volume   = { source_volume_key = "data" }
    }
  }

  assert {
    condition     = kacho_storage_image.this["from_snapshot"].source_snapshot_id == kacho_storage_snapshot.this["nightly"].id
    error_message = "образ не связан со снимком модуля по идентификатору"
  }
  assert {
    condition     = kacho_storage_image.this["from_volume"].source_volume_id == kacho_storage_volume.this["data"].id
    error_message = "образ не связан с томом модуля по идентификатору"
  }
}

# Регион задан ОДНОЙ переменной намеренно: образы набора — его региональная проекция, и
# расхождение регионов между двумя образами одного набора не задумано никогда.
run "region_is_shared_by_every_image" {
  command = plan

  variables {
    snapshots = { nightly = { source_volume_key = "data" } }
    images = {
      from_snapshot = { source_snapshot_key = "nightly" }
      from_volume   = { source_volume_key = "data" }
    }
  }

  assert {
    condition = alltrue([
      for _, i in kacho_storage_image.this : i.region_id == var.region_id
    ])
    error_message = "регион разошёлся между образами одного набора"
  }
}

# Зона — СВОЯ у каждого тома, а не общая на модуль: разнесение по зонам и есть смысл
# зональной посадки, и общая переменная запрещала бы то, ради чего зоны существуют.
run "zone_is_per_volume" {
  command = plan

  variables {
    volumes = {
      data_a = { zone_id = "ru-central1-a", size_bytes = 10737418240, disk_type_id = "network-ssd" }
      data_b = { zone_id = "ru-central1-b", size_bytes = 21474836480, disk_type_id = "network-ssd" }
    }
  }

  assert {
    condition     = kacho_storage_volume.this["data_a"].zone_id == "ru-central1-a"
    error_message = "зона первого тома не доехала"
  }
  assert {
    condition     = kacho_storage_volume.this["data_b"].zone_id == "ru-central1-b"
    error_message = "зона второго тома не доехала — зоны схлопнулись в одну"
  }
  assert {
    condition     = kacho_storage_volume.this["data_b"].size_bytes == 21474836480
    error_message = "размер тома потерян при сборке"
  }
}

# Метки ресурса накладываются ПОВЕРХ общих: одноимённый ключ у ресурса сильнее.
run "labels_merge_resource_over_module" {
  command = plan

  variables {
    labels = { owner = "platform", env = "probe" }
    volumes = {
      data = {
        zone_id      = "ru-central1-a"
        size_bytes   = 10737418240
        disk_type_id = "network-ssd"
        labels       = { owner = "storage-team", tier = "hot" }
      }
    }
    snapshots = { nightly = { source_volume_key = "data" } }
  }

  assert {
    condition     = kacho_storage_volume.this["data"].labels["owner"] == "storage-team"
    error_message = "метка ресурса не перекрыла общую"
  }
  assert {
    condition     = kacho_storage_volume.this["data"].labels["env"] == "probe"
    error_message = "общая метка потеряна при слиянии"
  }
  assert {
    condition     = kacho_storage_volume.this["data"].labels["tier"] == "hot"
    error_message = "собственная метка ресурса потеряна при слиянии"
  }
  assert {
    condition     = kacho_storage_snapshot.this["nightly"].labels["env"] == "probe"
    error_message = "общая метка не доехала до снимка"
  }
}

# Внешний источник тома — ИДЕНТИФИКАТОР, а не ключ: том из снимка этого же набора замкнул
# бы граф блоков. Проба закрепляет, что заданный идентификатор доезжает дословно.
run "external_volume_source_survives_assembly" {
  command = plan

  variables {
    volumes = {
      restored = {
        zone_id            = "ru-central1-a"
        size_bytes         = 10737418240
        disk_type_id       = "network-ssd"
        source_snapshot_id = "snpprobe00000000000"
      }
      seeded = {
        zone_id         = "ru-central1-a"
        size_bytes      = 21474836480
        disk_type_id    = "network-ssd"
        source_image_id = "imgprobe00000000000"
      }
    }
  }

  assert {
    condition     = kacho_storage_volume.this["restored"].source_snapshot_id == "snpprobe00000000000"
    error_message = "внешний снимок-источник потерян при сборке"
  }
  assert {
    condition     = kacho_storage_volume.this["seeded"].source_image_id == "imgprobe00000000000"
    error_message = "внешний образ-источник потерян при сборке"
  }
  assert {
    condition     = kacho_storage_volume.this["seeded"].disk_type_id == "network-ssd"
    error_message = "тип диска потерян при сборке"
  }
}

# ОТРИЦАНИЯ. Без них положительные пробы зеленели бы и на модуле, принимающем что угодно.
#
# Каждый негатив отличается от ЗАКОННОГО тома ровно одним признаком — тем, который он и
# проверяет. Иначе проба зеленеет по чужой причине: том без размера, типа диска и с двумя
# источниками сразу отвергается любой из четырёх проверок, и снятие проверяемой останется
# незамеченным. Законный близнец здесь — том из блока `variables` наверху.

run "volume_with_two_sources_is_rejected" {
  command = plan

  variables {
    volumes = {
      confused = {
        zone_id            = "ru-central1-a"
        size_bytes         = 10737418240
        disk_type_id       = "network-ssd"
        source_snapshot_id = "snpprobe00000000000"
        source_image_id    = "imgprobe00000000000"
      }
    }
  }

  expect_failures = [var.volumes]
}

run "volume_without_size_is_rejected" {
  command = plan

  variables {
    volumes = {
      sizeless = { zone_id = "ru-central1-a", disk_type_id = "network-ssd" }
    }
  }

  expect_failures = [var.volumes]
}

# Том ИЗ ИСТОЧНИКА размер тоже обязан нести: край не выводит его ни из снимка, ни из образа.
# Здесь эта конфигурация принималась модулем и отвергалась краем — то есть проверка входа
# обещала возможность, которой нет.
run "sourced_volume_without_size_is_rejected" {
  command = plan

  variables {
    volumes = {
      restored = {
        zone_id            = "ru-central1-a"
        disk_type_id       = "network-ssd"
        source_snapshot_id = "snpprobe00000000000"
      }
    }
  }

  expect_failures = [var.volumes]
}

# Тип диска обязателен: умолчания край не подставляет.
run "volume_without_disk_type_is_rejected" {
  command = plan

  variables {
    volumes = {
      typeless = { zone_id = "ru-central1-a", size_bytes = 10737418240 }
    }
  }

  expect_failures = [var.volumes]
}

run "non_positive_volume_size_is_rejected" {
  command = plan

  variables {
    volumes = {
      empty_size = { zone_id = "ru-central1-a", size_bytes = 0, disk_type_id = "network-ssd" }
    }
  }

  expect_failures = [var.volumes]
}

# Ключ, которого нет в карте томов, обязан отвергаться ВХОДОМ, а не «Invalid index» в теле
# ресурса: тот отказ не называет ни карты, ни известных ключей.
run "snapshot_of_unknown_volume_is_rejected" {
  command = plan

  variables {
    snapshots = { nightly = { source_volume_key = "no_such_volume" } }
  }

  expect_failures = [var.snapshots]
}

run "image_without_source_is_rejected" {
  command = plan

  variables {
    images = { golden = {} }
  }

  expect_failures = [var.images]
}

run "image_with_two_sources_is_rejected" {
  command = plan

  variables {
    snapshots = { nightly = { source_volume_key = "data" } }
    images = {
      golden = { source_snapshot_key = "nightly", source_volume_key = "data" }
    }
  }

  expect_failures = [var.images]
}

run "image_from_unknown_snapshot_is_rejected" {
  command = plan

  variables {
    snapshots = { nightly = { source_volume_key = "data" } }
    images    = { golden = { source_snapshot_key = "no_such_snapshot" } }
  }

  expect_failures = [var.images]
}

run "image_from_unknown_volume_is_rejected" {
  command = plan

  variables {
    images = { golden = { source_volume_key = "no_such_volume" } }
  }

  expect_failures = [var.images]
}
