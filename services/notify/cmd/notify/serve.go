// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/signal"
	"syscall"
	"time"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/observability/health"
	"github.com/PRO-Robotech/corelib/retention"
	"github.com/PRO-Robotech/corelib/servicehost"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/dkimkey"
	"github.com/PRO-Robotech/kacho/services/notify/internal/dnscheck"
	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

// sweepStopBound — сколько останов ждёт текущий проход уборки: партия —
// один оператор под серверным пределом TxStatementTimeout, проход — не больше
// retention.DefaultMaxBatchesPerPass партий; ждать дольше одного оператора с
// запасом останову незачем — недоделанная партия откатывается сервером.
const sweepStopBound = limits.TxStatementTimeout + 5*time.Second

// runServe — подъём процесса после стража конфигурации (main: шаг 1 —
// загрузчик, включая ручки DNS, пару DKIM и зону стенда). Порядок — замысел
// §12а «Порядок подъёма» (CX1-131 (а)):
//
//  2. дескриптор посадки и удостоверение пира — отказ до всякой поверхности;
//  3. агрегат здоровья с компонентами `database` и `dns`: оба «не готов», пока
//     их носитель не установлен;
//  4. диагностическая поверхность (`/healthz`, `/readyz`, `/metrics`) поднята;
//  5. страж DNS установки со сроком KACHO_NOTIFY_DNS_BOOT_DEADLINE: всё время
//     ожидания `/healthz` отвечает «жив», `/readyz` — «не готов» (NTF1-P08).
//     Нарушение или исчерпание срока — возврат ошибки с именем проверки;
//  6. прошёл — `dns` готов; пул базы, ограда ключа сетки, уборка, перепроверка;
//  7. цикл доставки (полоса A2): сборка, право kaname, рендер, подпись DKIM
//     парой стража, отправитель, исполнители строк и циклы источников. Он
//     стоит последним: письмо подписывается парой, которую страж уже проверил
//     по DNS, а резерв сетки идёт под записанной оградой ключа.
//
// Страж стоит после поверхности: liveness чарта (~50 с) короче срока стража
// (поставляемый 2 мин, верхняя граница 10 мин), и страж до поверхности дал бы
// цикл перезапусков без отказа с именем. Резолвер — параметр: main передаёт
// net.DefaultResolver (резолвер пода), пробы — резолвер на зону испытания.
func runServe(cfg config.Config, logger *slog.Logger, resolver *net.Resolver) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	desc, err := describe(cfg, logger)
	if err != nil {
		return err
	}
	if err := checkPeerTLS(cfg); err != nil {
		return err
	}

	posture, err := bootPosture(cfg, desc)
	if err != nil {
		return fmt.Errorf("самоотчёт о посадке: %w", err)
	}
	observability.LogBootPosture(logger, posture)

	reg := newRegistry()

	guard, err := dnscheck.New(resolver, dnscheck.Options{
		FromDomain:   cfg.FromDomain(),
		KeyFile:      cfg.DKIMKeyFile,
		SelectorFile: cfg.DKIMSelectorFile,
		FS:           dkimkey.OS,
		Clock:        dnscheck.SystemClock,
		Registerer:   reg,
		Logger:       logger,
	})
	if err != nil {
		return err
	}

	// Носители компонентов появляются ПОЗЖЕ поверхности: до установки слот
	// отвечает «не готов» (fail-closed), а не «исправен».
	var dbSlot, dnsSlot health.Slot
	agg := health.New([]health.Checker{dbSlot.Checker("database"), dnsSlot.Checker("dns")})
	go func() {
		<-ctx.Done()
		agg.SetShuttingDown()
	}()

	diag, err := describeDiagnosticSurface(cfg.DiagAddr, reg, agg, desc.Spec().Mode, logger)
	if err != nil {
		return fmt.Errorf("профиль диагностической поверхности: %w", err)
	}
	wait, err := servicehost.ServeSurface(ctx, diag)
	if err != nil {
		return fmt.Errorf("диагностическая поверхность: %w", err)
	}
	// abort — отказ подъёма после поверхности: поверхность гасится, ошибка
	// уходит наверх и останавливает процесс ненулевым кодом.
	abort := func(err error) error {
		cancel()
		_ = wait()
		return err
	}

	if err := guard.Boot(ctx, cfg.DNSBootDeadline); err != nil {
		return abort(err)
	}
	dnsSlot.Install(func(context.Context) error { return nil })

	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return abort(fmt.Errorf("пул базы %s: %w", cfg.DBName, err))
	}
	defer pool.Close()
	dbSlot.Install(func(ctx context.Context) error { return pool.Ping(ctx) })

	// Ограда ключа сетки — после миграций (init-контейнер точки наката) и до
	// первого `Claim`: действующий ключ задаёт последняя стартовавшая реплика
	// (З24, CX1-68). Отказ записи, включая предел ожидания замка, — отказ
	// старта ненулевым кодом; журнал называет время ожидания и не несёт ни
	// ключа, ни отпечатка.
	lim, err := limits.New(limits.Options{
		Pool:       pool,
		Key:        cfg.RecipientKey().Bytes(),
		Grid:       cfg.Grid(),
		Now:        time.Now,
		Registerer: reg,
		Logger:     logger,
	})
	if err != nil {
		return abort(fmt.Errorf("сетка лимитов: %w", err))
	}
	if err := lim.WriteFence(ctx); err != nil {
		return abort(err)
	}
	// Уборка окон сетки и суток потолка, закончившихся раньше порога (§6, З24).
	sweeper, err := retention.New(retention.DefaultConfig(), lim.RetentionSubjects(), logger)
	if err != nil {
		return abort(fmt.Errorf("уборка лимитов: %w", err))
	}
	sweeper.Start(ctx)

	// Перепроверка DNS установки и перечитывание пары DKIM на её такте
	// (NTF1-P10, P11, P17, P18): остановить отправку у неё пути нет.
	recheckDone := make(chan struct{})
	go func() {
		defer close(recheckDone)
		guard.Run(ctx, cfg.DNSRecheckInterval)
	}()

	del, err := startDelivery(ctx, cfg, deliveryDeps{Pairs: guard, Limiter: lim, Registry: reg, Log: logger})
	if err != nil {
		cancel()
		<-recheckDone
		sweeper.Wait(sweepStopBound)
		_ = wait()
		return fmt.Errorf("цикл доставки: %w", err)
	}

	<-ctx.Done()
	logger.Info("останов по сигналу")
	// Строки в полёте доводятся до Ack под своим сроком (конец аренды, З21):
	// пул базы (сетка) и поверхность живы, пока они не доведены.
	del.Wait()
	<-recheckDone
	if !sweeper.Wait(sweepStopBound) {
		logger.Warn("петля уборки лимитов не завершилась за предел останова", "bound", sweepStopBound)
	}
	return wait()
}
