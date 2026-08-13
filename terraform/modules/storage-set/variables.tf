# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

variable "project_id" {
  description = "Проект-владелец томов, снимков и образов набора. Сменить его у существующего ресурса нельзя."
  type        = string
}

variable "region_id" {
  description = <<-EOT
    Регион ОБРАЗОВ набора.

    Одна переменная на все образы, а не поле у каждого образа: образ здесь — региональная
    проекция томов этого же набора, и расхождение регионов между двумя образами одного
    набора не задумано никогда. Две переменные позволили бы его задать, а обнаружилось бы
    оно только на apply.

    ПРАВИЛО КОГЕРЕНТНОСТИ, которое обязан соблюсти вызывающий: зона каждого тома,
    из которого — прямо или через снимок — делается образ, обязана принадлежать ЭТОМУ
    региону. Иначе край отвергнет создание образа.

    Модуль это НЕ проверяет и проверить не может: соответствие зон регионам знает только
    край (справочник geo), а выводить регион разбором имени зоны запрещено — имена зон и
    регионов произвольны, между ними нет гарантированного отношения, и такая проверка
    молча проходила бы всегда, выглядя исполненной.
  EOT
  type        = string
}

variable "labels" {
  description = <<-EOT
    Метки, применяемые ко ВСЕМ ресурсам набора.

    Метки отдельного ресурса накладываются поверх этих: одноимённый ключ у ресурса
    сильнее общего.
  EOT
  type        = map(string)
  default     = {}
}

variable "volumes" {
  description = <<-EOT
    Тома набора. Ключ карты — имя тома в пределах проекта.

    Источник тома задаётся ВНЕШНИМ идентификатором (`source_snapshot_id` /
    `source_image_id`), а не ключом карт этого модуля, и это не упущение: снимок уже
    ссылается на том, поэтому обратная ссылка тома на снимок замкнула бы граф блоков —
    конфигурация была бы отвергнута циклом ещё до обращения к краю. Том, выращенный из
    снимка ЭТОГО набора, заводится вторым экземпляром модуля: идентификатор снимка отдан
    выходом `snapshot_ids`.

    Внутри набора ссылка идёт по КЛЮЧУ (ресурса ещё нет, идентификатора тоже),
    за его пределы — по ИДЕНТИФИКАТОРУ (ресурс уже существует).

    Зона задаётся У КАЖДОГО ТОМА, а не одной переменной на модуль: разнесение томов по
    зонам — это и есть смысл зональной посадки, и набор одного проекта законно живёт в
    нескольких зонах одного региона. Общая переменная запрещала бы ровно то, ради чего
    зоны существуют. Зона неизменяема: её смена пересоздаёт том, а с ним пропадают данные.

    `size_bytes` и `disk_type_id` обязательны у КАЖДОГО тома — включая том из снимка и том
    из образа. Край не выводит их из источника: размер обязан быть больше нуля, а тип диска
    — позицией справочника, и оба поля проверяются синхронно, до всякой записи. У тома из
    образа размер обязан быть вдобавок не меньше `minDiskBytes` этого образа; величину знает
    только край, поэтому модуль её не сверяет и не подставляет.

    Здесь стояло обратное: размер объявлялся необязательным при заданном источнике —
    «наследовать его не у чего» только у пустого тома. Наследования нет ни у какого, и та
    редакция принимала конфигурацию, которую край отвергает всегда.

    Умолчания у размера НЕТ намеренно: том нельзя уменьшить, поэтому подставленное за
    вызывающего значение исправляется только пересозданием — то есть потерей данных.

    Обязательность выражена ПРОВЕРКАМИ ниже, а не обязательными атрибутами типа объекта, и
    это не стилистика: пропуск обязательного атрибута край HCL сообщает ошибкой, которая ни
    к какой переменной не приписана, поэтому `expect_failures` её не ловит и отрицательной
    пробы на неё не написать вовсе. Проверка называет виновные тома, объясняет причину и
    роняет пробу.
  EOT
  type = map(object({
    zone_id            = string
    size_bytes         = optional(number)
    disk_type_id       = optional(string)
    source_snapshot_id = optional(string)
    source_image_id    = optional(string)
    description        = optional(string, "")
    labels             = optional(map(string), {})
  }))
  default = {}

  validation {
    condition = alltrue([
      for _, v in var.volumes :
      v.source_snapshot_id == null || v.source_image_id == null
    ])
    error_message = format(
      "У томов %s заданы оба источника сразу. source_snapshot_id и source_image_id взаимоисключающи: том растёт ровно из одного источника.",
      join(", ", [for k, v in var.volumes : k if v.source_snapshot_id != null && v.source_image_id != null])
    )
  }

  validation {
    condition = alltrue([for _, v in var.volumes : v.size_bytes != null])
    error_message = format(
      "У томов %s не задан size_bytes. Размер обязателен у КАЖДОГО тома, включая том из снимка и том из образа: край не выводит его из источника, а у тома из образа он обязан быть ещё и не меньше minDiskBytes образа.",
      join(", ", [for k, v in var.volumes : k if v.size_bytes == null])
    )
  }

  validation {
    condition = alltrue([for _, v in var.volumes : v.disk_type_id != null && v.disk_type_id != ""])
    error_message = format(
      "У томов %s не задан disk_type_id. Тип диска обязателен у каждого тома: умолчания край не подставляет и отвергает создание синхронно.",
      join(", ", [for k, v in var.volumes : k if v.disk_type_id == null || v.disk_type_id == ""])
    )
  }

  validation {
    condition = alltrue([for _, v in var.volumes : v.size_bytes == null || v.size_bytes > 0])
    error_message = format(
      "У томов %s size_bytes не положителен. Размер задаётся в БАЙТАХ и обязан быть больше нуля.",
      join(", ", [for k, v in var.volumes : k if v.size_bytes != null && v.size_bytes <= 0])
    )
  }
}

variable "snapshots" {
  description = <<-EOT
    Снимки набора. Ключ карты — имя снимка в пределах проекта.

    Снимок снимается С ТОМА, и том называется КЛЮЧОМ карты `volumes`, а не
    идентификатором: идентификатор присваивается краем при создании, поэтому у тома,
    которого ещё нет, его нет и у вызывающего. Ключ модуль разрешает в идентификатор сам —
    и тем строит граф.
  EOT
  type = map(object({
    source_volume_key = string
    description       = optional(string, "")
    labels            = optional(map(string), {})
  }))
  default = {}

  validation {
    condition = alltrue([
      for _, s in var.snapshots : contains(keys(var.volumes), s.source_volume_key)
    ])
    error_message = format(
      "Снимки %s ссылаются на том, которого нет в карте volumes. Известные ключи: %s.",
      join(", ", [for k, s in var.snapshots : k if !contains(keys(var.volumes), s.source_volume_key)]),
      length(var.volumes) == 0 ? "карта volumes пуста" : join(", ", keys(var.volumes))
    )
  }
}

variable "images" {
  description = <<-EOT
    Образы набора. Ключ карты — имя образа в пределах проекта.

    Источник образа — РОВНО ОДИН: снимок набора (`source_snapshot_key`) либо том набора
    (`source_volume_key`). Оба называются ключом своей карты, по той же причине, что и у
    снимка: идентификатора у несозданного ресурса нет.

    Внешнего источника у образа НЕТ намеренно. Образ из чужого тома завёл бы в набор
    источник в неизвестном регионе — и общий `region_id` модуля перестал бы быть тем, чем
    он объявлен: единственной опорой правила когерентности. Такой образ заводится ресурсом
    рядом с модулем, где его регион виден вызывающему целиком.
  EOT
  type = map(object({
    source_snapshot_key = optional(string)
    source_volume_key   = optional(string)
    description         = optional(string, "")
    labels              = optional(map(string), {})
  }))
  default = {}

  validation {
    condition = alltrue([
      for _, i in var.images :
      (i.source_snapshot_key == null) != (i.source_volume_key == null)
    ])
    error_message = format(
      "У образов %s задано не ровно одно из source_snapshot_key и source_volume_key. Образ делается либо из снимка, либо из тома.",
      join(", ", [
        for k, i in var.images : k
        if length([for v in [i.source_snapshot_key, i.source_volume_key] : v if v != null]) != 1
      ])
    )
  }

  validation {
    condition = alltrue([
      for _, i in var.images :
      i.source_snapshot_key == null || contains(keys(var.snapshots), i.source_snapshot_key)
    ])
    error_message = format(
      "Образы %s ссылаются на снимок, которого нет в карте snapshots. Известные ключи: %s.",
      join(", ", [
        for k, i in var.images : k
        if i.source_snapshot_key != null && !contains(keys(var.snapshots), i.source_snapshot_key)
      ]),
      length(var.snapshots) == 0 ? "карта snapshots пуста" : join(", ", keys(var.snapshots))
    )
  }

  validation {
    condition = alltrue([
      for _, i in var.images :
      i.source_volume_key == null || contains(keys(var.volumes), i.source_volume_key)
    ])
    error_message = format(
      "Образы %s ссылаются на том, которого нет в карте volumes. Известные ключи: %s.",
      join(", ", [
        for k, i in var.images : k
        if i.source_volume_key != null && !contains(keys(var.volumes), i.source_volume_key)
      ]),
      length(var.volumes) == 0 ? "карта volumes пуста" : join(", ", keys(var.volumes))
    )
  }
}
