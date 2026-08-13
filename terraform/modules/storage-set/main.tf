# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

terraform {
  # 1.9, а не 1.6 как у соседних модулей: проверки входа здесь ссылаются на ДРУГУЮ
  # переменную (ключ снимка обязан быть ключом карты томов). Межпеременные validation
  # появились в 1.9; на 1.6 такая проверка не разбирается вовсе.
  #
  # Альтернативой была бы precondition у ресурса, но она проигрывает гонку: обращение
  # `kacho_storage_volume.this[<чужой ключ>]` в теле ресурса даёт «Invalid index» — отказ,
  # который не называет ни карты, ни известных ключей. Проверка входа срабатывает раньше
  # любого ресурса и печатает и то, и другое.
  required_version = ">= 1.9"

  required_providers {
    kacho = {
      source = "PRO-Robotech/kacho"
    }
  }
}

# Каждый атрибут присваивается ОТДЕЛЬНО, а не переданной целиком переменной. У ресурсов
# storage вложенных объектов в схеме сегодня нет, поэтому «Value Conversion Error» этой
# формы здесь пока не возникает; правило соблюдается всё равно — вложенный объект появится
# в схеме раньше, чем кто-нибудь вспомнит про исключение из правила.
resource "kacho_storage_volume" "this" {
  for_each = var.volumes

  project_id  = var.project_id
  name        = each.key
  description = each.value.description
  labels      = merge(var.labels, each.value.labels)

  zone_id      = each.value.zone_id
  disk_type_id = each.value.disk_type_id
  size_bytes   = each.value.size_bytes

  # Источники — ВНЕШНИЕ идентификаторы: ссылка на снимок этого же модуля замкнула бы граф
  # блоков (снимок уже ссылается на том), и конфигурация была бы отвергнута циклом.
  source_snapshot_id = each.value.source_snapshot_id
  source_image_id    = each.value.source_image_id
}

# Снимок ссылается на том ПО ИДЕНТИФИКАТОРУ, а ключ карты разрешается здесь. Ссылка строит
# граф: создание идёт том → снимок, снос — в обратном порядке.
#
# Порядок несущий, но обоснование у него ОБРАТНОЕ ожидаемому, и здесь стояло ложное: «край
# не удаляет том, пока с него сняты снимки». Не так. Снимок ПЕРЕЖИВАЕТ свой том —
# `snapshots.source_volume_id` объявлен `ON DELETE SET NULL` (миграция 0003 домена storage),
# — поэтому неверный порядок ничем не отвергается: край молча обнуляет источник снимка. На
# отказ края опереться нельзя, и порядок держится ровно ссылкой, больше ничем. Ложное
# обоснование хуже отсутствующего: оно обещает громкий отказ там, где происходит тихая
# потеря происхождения.
resource "kacho_storage_snapshot" "this" {
  for_each = var.snapshots

  project_id  = var.project_id
  name        = each.key
  description = each.value.description
  labels      = merge(var.labels, each.value.labels)

  source_volume_id = kacho_storage_volume.this[each.value.source_volume_key].id
}

# Образ ссылается на снимок ЛИБО на том — тоже по идентификатору. Тем самым порядок сноса
# получается полным: образы → снимки → тома.
#
# Край и здесь не держит: `images.source_snapshot_id` и `images.source_volume_id` объявлены
# `ON DELETE SET NULL` (миграция 0007). То есть ни один из трёх ресурсов набора не защищён
# от сноса своим потомком — весь порядок выражен графом ссылок и держится только им.
resource "kacho_storage_image" "this" {
  for_each = var.images

  project_id  = var.project_id
  region_id   = var.region_id
  name        = each.key
  description = each.value.description
  labels      = merge(var.labels, each.value.labels)

  source_snapshot_id = (
    each.value.source_snapshot_key == null
    ? null
    : kacho_storage_snapshot.this[each.value.source_snapshot_key].id
  )
  source_volume_id = (
    each.value.source_volume_key == null
    ? null
    : kacho_storage_volume.this[each.value.source_volume_key].id
  )
}
