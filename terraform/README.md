<!-- Copyright (c) PRO-Robotech -->
<!-- SPDX-License-Identifier: BUSL-1.1 -->

# Terraform-провайдер Kachō

Описывает ресурсы платформы декларативно. Работает и с Terraform, и с OpenTofu — протокол
плагинов у них общий.

```
terraform/
├── cmd/terraform-provider-kacho/   двоичный файл плагина
├── internal/client/                клиент края: транспорт, операции, чтение
├── internal/provider/              провайдер и ресурсы
├── modules/vpc-network/            модуль «сеть с подсетями»
└── examples/basic/                 наименьшая рабочая конфигурация
```

## Часть общего модуля — и почему это так

Провайдер живёт в **том же** Go-модуле, что и продукт, рядом с сервисами. Следствия, ради
которых так и сделано:

- **типы контракта берутся из `pkg/api` напрямую** — второго выхода генерации нет, значит
  нечему устаревать и не за чем следить гейтом;
- **общие библиотеки доступны как есть**: закон повторов — `pkg/backoff`, каталог префиксов
  идентификаторов — `pkg/ids`. Провайдер не заводит своих копий того, что у платформы уже
  есть;
- **проверки те же, что у продукта**: `go build ./...`, `go vet`, `golangci-lint`,
  `govulncheck`, `gosec` и гейт отложенной работы обходят его вместе со всем деревом.
  Отдельная джоба нужна ровно одна — формат HCL, потому что `go vet` его не читает.

Цена измерена, а не предположена: корневой `go.sum` вырос с 362 строк до 407 (+45, из них
18 `hashicorp`), а в бинари и образы сервисов код провайдера **не попадает** — граф
импортов сервиса не содержит ни одной строки `hashicorp`, потому что Go собирает только
импортируемое.

## Сборка и локальное подключение

```bash
go build -o ~/.kacho/plugins/terraform-provider-kacho ./terraform/cmd/terraform-provider-kacho
```

```hcl
# ~/.tofurc (или ~/.terraformrc)
provider_installation {
  dev_overrides { "PRO-Robotech/kacho" = "/home/<вы>/.kacho/plugins" }
  direct {}
}
```

```bash
export KACHO_ENDPOINT=https://api.kacho.example
export KACHO_TOKEN=<токен, выпущенный IAM>
cd terraform/examples/basic && tofu apply -var project_id=<проект>
```

## Проверки

```bash
go test ./terraform/... -race
tofu fmt -check -recursive terraform/modules terraform/examples
```

## Документация

Пользовательские страницы — в docs-site сервиса VPC, раздел «Terraform». Контракт
под-фазы — приёмка `sub-phase-TF-1-terraform-provider-vpc-core-acceptance.md` в
репозитории воркспейса.
