<!--
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
-->

# kacho-notify

**Шлюз уведомлений платформы Kachō.** Единственный процесс, который держит креды
почты: письма он не принимает вызовом, а сам забирает из лент источников, у
каждого письма спрашивает право у kaname и отправляет его.

## Поверхность

Каталог несёт три корня процессов (правило счёта процессов гейтов дерева —
`internal/repohygiene/processroots.go`) и точку наката. Ведомость «корень процесса →
обслуживаемые gRPC-сервисы» — `services/notify/servesurface_ledger.go`.

- `cmd/notify` — шлюз (`kacho-notify serve`). **Входящего RPC нет**: gRPC-сервисов
  он не обслуживает и gRPC-слушателя не поднимает. Единственная поверхность —
  диагностический HTTP (`/healthz`, `/readyz`, `/metrics`) на адресе ручки
  `KACHO_NOTIFY_DIAG_ADDR`, досягаемом только внутри кластера. Ручки — переменные
  `KACHO_NOTIFY_*` без умолчаний (`services/notify/internal/config`).
- `cmd/notify-api` — развёртывание запросов оператора и арендаторов
  (`kacho-notify-api serve`, kacho#2924, приёмка NTF-5 Р2). Единственный
  **внутренний** mTLS-слушатель носителя в форме «только внутренний»: внутренний
  сервис извещений `kacho.cloud.notify.v1.InternalNoticeService`, их чтение
  арендатором `kacho.cloud.notify.v1.NoticeService` и опрос операций
  `corelib.operation.OperationService`. Публичного слушателя нет по построению:
  край маршрутизирует на этот слушатель по одному соединению. Секрета почты и
  справочника службы доступа у развёртывания нет. Ручки — свои, объявлены под
  корнем (`cmd/notify-api/internal/config`), без умолчаний.
- `cmd/notify-probe` — стендовая проба-источник (`kacho-notify-probe serve`, база
  `kacho_notifyprobe`): на **внутреннем** слушателе служит глагол
  `kacho.cloud.notify.v1.InternalNotifyProbeService/Send` (поставить письмо
  `probe-hello`, ответ — `notification_id` закоммиченной строки), ленту
  `corelib.notify.InternalNotificationFeedService` и подписку
  `corelib.subscription.InternalSubscriptionService` — две последние только при
  `KACHO_NOTIFYPROBE_NOTIFICATIONS_ENABLED=true`. Все три — `Internal*`, край их не
  маршрутизирует (вид `notification_feed` только внутренний).
- `cmd/migrator` — точка наката (`kacho-migrator up`): цепочку выбирает по имени
  базы в DSN по таблице `cmd/migrator/chains.yaml`, DSN — только `--dsn` или
  `KACHO_MIGRATOR_DSN`.

Образ каталога один (`kacho-notify`) и несёт четыре бинаря.

Слой use-case — `internal/apps/kacho/api/{notice,publicnotice}` (метод — пакет);
хранилище извещений — `internal/repo/noticerepo`; правила видов, переходов и
напоминаний, общие обоим развёртываниям, — `internal/notice/rules`.

## Решения о поверхности, которой у службы нет

Снаружи «решено не заводить» и «ещё не сделано» неразличимы, поэтому каждое такое
решение записано здесь и держится гейтом дерева, который роняет прогон, как только
решение перестаёт быть правдой.

### Глагол подписки служит проба, а не шлюз

Поток `corelib.subscription.InternalSubscriptionService/Subscribe` шлюз notify
**открывает** к источникам, а не служит; служит его в каталоге только проба-источник
(вид `notification_feed`), и только на внутреннем слушателе. Клиентской
документации об этом виде нет и быть не должно: клиенту он недоступен. Держат
`TestEveryDomainEitherServesSubscriptionOrRecordsWhyNot` и
`TestSubscriptionOwnersSaySoInTheirClientDocs` (запись внутреннего владельца
истекает, когда край узнает адрес домена notify).

### Решения, истёкшие с notify-api

Решения «публичного списка нет» и «слоя use-case под `internal/apps/` нет»
(kacho#2915) сняты: notify-api служит `List*` и несёт слой use-case. Держат
`TestCoverage_PremiseEveryServiceHasAListingSurface` (требует анализатор отбора
списков службы, цель Makefile и шаг конвейера) и `TestUseCaseLayoutPremiseHolds`
(раскладка судится как у всех служб).

## Сборка

```bash
make build    # → bin/kacho-notify
make test     # прогон через корневой Makefile
make docker   # образ kacho-notify:dev, контекст — корень монорепо
```
