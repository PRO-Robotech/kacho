// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package jobs — фоновые workers сервиса kacho-nlb.
//
// Собираются в assembleBackgroundWorkers (cmd/kacho-loadbalancer/wiring.go) и
// исполняются errgroup'ом в runServe. Прежняя редакция этого комментария
// называла здесь parallel.ExecAbstract: в kacho-nlb это имя встречается только в
// комментариях (предикат: `git grep -n ExecAbstract -- services/nlb` — четыре
// совпадения, все в прозе, ни одного вызова; для контроля в обратную сторону —
// services/iam/cmd/kaname/serve.go, где вызов настоящий). Механизм, названный
// только в комментарии, по грепу неотличим от живого.
//
// Реализованные — их ДВА, и оба поднимаются одним и тем же набором:
//
//   - target_drain_runner.go — двухфазный drain. Периодически
//     (interval из cfg.Jobs.TargetDrain.Interval, default 10s) делает
//     `DELETE FROM kacho_nlb.targets WHERE status='DRAINING' AND
//     drain_started_at < now - tg.deregistration_delay_seconds` + INSERT
//     DISTINCT outbox `nlb_target_group:<tg_id> UPDATED`. Логирует каждый
//     tick (deleted/tgs/took_ms). Transient errors → log + continue;
//     ctx cancel → штатное завершение.
//
//   - free_ip_runner.go — возврат аренды VIP по застрявшим балансировщикам
//     (multi-replica-safe claim через FOR UPDATE SKIP LOCKED). Сканирует
//     ТОЛЬКО kacho_nlb.load_balancers и выбирает ветку освобождения по
//     дискриминатору источника: 'auto' → ClearReference+FreeIP, 'linked' →
//     только ClearReference. Разбор — docs/engineering/architecture/15-free-ip-runner.md.
//     Требует vpc internal-address client: без него не собирается вовсе, иначе
//     аренда течёт молча.
//
// Раздела «Запланированные» здесь больше нет, и он не должен появиться снова.
// Он обещал два worker'а, «отдельными tracking-issues, не в текущем PR»: слив
// outbox для потока жизненного цикла и писателя кортежей в модель прав. Оба
// предмета к моменту снятия УЖЕ существовали под другими именами — слив очереди
// регистраций и его реконсайлер собираются в том же wiring.go, — то есть
// обещание пережило собственную нужду и продолжало ставить в очередь работу,
// которой не требовалось. Отложенной работы в дереве не держим (ban #11); её
// отсутствие держит гейт internal/repohygiene.
package jobs
