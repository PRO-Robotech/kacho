<!-- Copyright (c) PRO-Robotech -->
<!-- SPDX-License-Identifier: BUSL-1.1 -->

# Terraform-провайдер Kachō

Описывает ресурсы платформы декларативно. Работает и с Terraform, и с OpenTofu — протокол
плагинов у них общий.

```
terraform/
├── cmd/terraform-provider-kacho/   двоичный файл плагина
├── internal/api/                   типы контракта (ПОРОЖДЕНЫ, руками не править)
├── internal/client/                клиент края: транспорт, операции, чтение
├── internal/provider/              провайдер и ресурсы
├── modules/vpc-network/            модуль «сеть с подсетями»
└── examples/basic/                 наименьшая рабочая конфигурация
```

## Почему это отдельный Go-модуль

Дерево зависимостей `terraform-plugin-framework` велико. В едином модуле продукта оно вошло
бы в граф **каждого** сервиса и каждой сборки образа — выросли бы время сборки, поверхность
анализа уязвимостей и объём файла сумм. Модуль изолирован, и это свойство держит гейт
`TestProviderModuleIsIsolated` (в модуле продукта, не здесь), проверяющий шесть условий —
включая то, что корневой `go.sum` не приобрёл строк `hashicorp`.

Типы контракта приезжают **вторым выходом генерации** из общего каталога `proto/`
(`proto/buf.gen.terraform.yaml`) — только message-типы, под собственным префиксом пакета.
Поэтому провайдер не импортирует модуль продукта, а значит не нужен и `replace`.

## Сборка и локальное подключение

```bash
go build -o ~/.kacho/plugins/terraform-provider-kacho ./cmd/terraform-provider-kacho
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
cd examples/basic && tofu apply -var project_id=<проект>
```

## Проверки

```bash
go test ./... -race        # юниты: транспорт, классификатор, операция, чтение
go vet ./...
tofu fmt -check -recursive modules examples
```

Приёмочные тесты против стенда включаются переменной `TF_ACC` — без неё они пропускаются,
и число пропущенных печатается: «ноль находок» обязано быть отличимо от «ничего не
исполнялось».

## Документация

Пользовательские страницы — в docs-site сервиса VPC, раздел «Terraform»:
провайдер и модуль `vpc-network`. Контракт под-фазы — приёмка
`sub-phase-TF-1-terraform-provider-vpc-core-acceptance.md` в репозитории воркспейса.
