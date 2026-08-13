# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Модульные пробы: проверяют СВЯЗЫВАНИЕ и проверки входа, не обращаясь к краю.

mock_provider "kacho" {}

variables {
  project_id = "prjprobe000000000000"
  region_id  = "ru-central1"
  name       = "probe-reg"
}

run "repository_references_the_registry_by_id" {
  command = plan

  variables {
    repositories = { "team/service" = { description = "образы сервиса" } }
  }

  assert {
    condition     = kacho_registry_repository.this["team/service"].registry_id == kacho_registry_registry.this.id
    error_message = "репозиторий не связан с реестром модуля"
  }
  # Имя с косой чертой — законное: край объявляет его как многосегментный путь.
  assert {
    condition     = kacho_registry_repository.this["team/service"].repository == "team/service"
    error_message = "имя репозитория с косой чертой не доехало"
  }
}

# Умолчание модуля — приватный реестр, и это решение, а не наследование краю.
run "registry_defaults_to_private" {
  command = plan

  assert {
    condition     = kacho_registry_registry.this.default_repository_visibility == "PRIVATE"
    error_message = "умолчание видимости не PRIVATE — публичный доступ достался бы по невнимательности"
  }
}

# Видимость репозитория доезжает ДОСЛОВНО.
#
# Здесь стояла проба «незаданная видимость остаётся null» — она проверяла не модуль, а
# подменённого провайдера: он заполняет вычисляемые поля случайным значением, и наследование
# на подмене не выразимо вовсе. Утверждение о том, чего проба видеть не может, зеленеет или
# краснеет по причинам, не связанным с предметом.
run "repository_visibility_is_passed_through" {
  command = plan

  variables {
    repositories = {
      "public-one"  = { visibility = "PUBLIC" }
      "private-one" = { visibility = "PRIVATE" }
    }
  }

  assert {
    condition     = kacho_registry_repository.this["public-one"].visibility == "PUBLIC"
    error_message = "видимость репозитория не доехала до ресурса"
  }
  assert {
    condition     = kacho_registry_repository.this["private-one"].visibility == "PRIVATE"
    error_message = "видимость репозитория подменена по дороге"
  }
}

# ОТРИЦАНИЯ. Без них положительные пробы зеленели бы и на модуле, принимающем что угодно.

run "unknown_registry_visibility_is_rejected" {
  command = plan

  variables {
    default_repository_visibility = "VISIBILITY_PRIVATE"
  }

  expect_failures = [var.default_repository_visibility]
}

run "unknown_repository_visibility_is_rejected" {
  command = plan

  variables {
    repositories = { "bad" = { visibility = "private" } }
  }

  expect_failures = [var.repositories]
}
