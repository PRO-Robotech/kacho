<!--
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
-->

# kacho-notify

**Шлюз уведомлений платформы Kachō.** Единственный процесс, который держит креды
почты: письма он не принимает вызовом, а сам забирает из лент источников, у
каждого письма спрашивает право у kaname и отправляет его.

## Поверхность

У notify **нет входящего RPC**: gRPC-сервисов он не обслуживает и gRPC-слушателя не
поднимает. Единственная поверхность — диагностический HTTP (`/healthz`, `/readyz`,
`/metrics`) на адресе ручки `KACHO_NOTIFY_DIAG_ADDR`, досягаемом только внутри
кластера. Ведомость «корень процесса → обслуживаемые gRPC-сервисы» —
`services/notify/servesurface_ledger.go`.

Ручки — переменные `KACHO_NOTIFY_*` без умолчаний: незаданная обязательная ручка или
значение вне границы останавливает старт с именем ручки
(`services/notify/internal/config`).

## Решения о поверхности, которой у службы нет

Снаружи «решено не заводить» и «ещё не сделано» неразличимы, поэтому каждое такое
решение записано здесь и держится гейтом дерева, который роняет прогон, как только
решение перестаёт быть правдой.

### Два процесса каталога: шлюз и проба-источник

Каталог несёт два корня процессов (правило счёта процессов гейтов дерева —
`internal/repohygiene/processroots.go`) и точку наката:

- `cmd/notify` — шлюз (`kacho-notify serve`): описан выше, входящего RPC нет;
- `cmd/notify-probe` — стендовая проба-источник (`kacho-notify-probe serve`, база
  `kacho_notifyprobe`): на **внутреннем** слушателе служит глагол
  `kacho.cloud.notify.v1.InternalNotifyProbeService/Send` (поставить письмо
  `probe-hello`, ответ — `notification_id` закоммиченной строки), ленту
  `corelib.notify.InternalNotificationFeedService` и подписку
  `corelib.subscription.InternalSubscriptionService` — две последние только при
  `KACHO_NOTIFYPROBE_NOTIFICATIONS_ENABLED=true`. Все три — `Internal*`, край их не
  маршрутизирует (вид `notification_feed` только внутренний);
- `cmd/migrator` — точка наката (`kacho-migrator up`): цепочку выбирает по имени
  базы в DSN по таблице `cmd/migrator/chains.yaml`, DSN — только `--dsn` или
  `KACHO_MIGRATOR_DSN`.

Образ каталога один (`kacho-notify`) и несёт три бинаря.

### Глагол подписки служит проба, а не шлюз

Поток `corelib.subscription.InternalSubscriptionService/Subscribe` шлюз notify
**открывает** к источникам, а не служит; служит его в каталоге только проба-источник
(вид `notification_feed`), и только на внутреннем слушателе. Клиентской
документации об этом виде нет и быть не должно: клиенту он недоступен. Держат
`TestEveryDomainEitherServesSubscriptionOrRecordsWhyNot` и
`TestSubscriptionOwnersSaySoInTheirClientDocs` (запись внутреннего владельца
истекает, когда край узнает адрес домена notify).

### Публичного списка нет — анализатора отбора списков нет

Ни один процесс каталога не служит `List*`: отбирать нечего, анализатор судил бы
пустоту.
Держит `TestCoverage_PremiseEveryServiceHasAListingSurface`: первый `List*` в
транспорте службы снимает решение, и гейт требует анализатор, цель Makefile и шаг
конвейера.

### Слоя use-case под `internal/apps/` нет

Use-case ресурса класть некуда: ресурса у службы нет, глагол пробы `Send` —
стендовая постановка одной транзакцией, а рабочие циклы службы — подписка, забор и
отметка писем, отправка — не обработчики глагола. Держит `TestUseCaseLayoutPremiseHolds`:
появится `internal/apps/` — решение снимается, и предпосылка единственной раскладки
судится как у всех служб. Запрет второй раскладки (`TestUseCaseLayerHasOneLayout`)
notify судит наравне со всеми.

Все три решения разобраны в задаче kacho#2915.

## Сборка

```bash
make build    # → bin/kacho-notify
make test     # прогон через корневой Makefile
make docker   # образ kacho-notify:dev, контекст — корень монорепо
```
