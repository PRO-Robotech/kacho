// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// admission.go — проводка ограничителя допуска запросов на ОБА листенера.
//
// # Почему обёртка регистратора, а не звено цепочки
//
// Носитель контура собирает цепочку звеньев сам и сервера вызывающему не отдаёт
// — это его несущее свойство, а не недосмотр. Обёртка регистратора работает в
// той же модели: она получает дескриптор службы и подставляет допуск МЕЖДУ
// цепочкой и обработчиком. Место выбрано не из удобства — ключом служит личность,
// которую устанавливают звенья цепочки (личность сертификата → круг доверенных
// отправителей → личность конечного пользователя). Ограничитель, ключующийся
// раньше этого решения, снимается подстановкой чужого заголовка, то есть
// ограничивает только того, кто не пытается его обойти.
//
// # Почему у листенеров РАЗНЫЕ ключи, и это названо здесь
//
//   - публичный — по личности конечного пользователя: за краем сидит арендатор,
//     и предел объявлен на него;
//   - внутренний — по личности СЕРТИФИКАТА: его зовут наши модули, и запрос
//     одного модуля несёт личности разных арендаторов. Ключ по арендатору дробил
//     бы бюджет соседа на тысячу вёдер и душил бы его на ровном месте.
//
// Величины внутреннего листенера заведомо выше рабочих по той же причине, по
// которой у него другой ключ: ограничитель, задушивший наш собственный поток
// намерения, воспроизводит заклинивание головы очереди — класс, при котором
// работа перестаёт доезжать без единого видимого симптома.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
)

// admissionReportInterval — как часто в журнал уходит счёт допущенных и
// отвергнутых.
//
// Отчёт печатается ВСЕГДА, а не только при находках: «ноль отказов за всю жизнь
// контроля» обязано быть заметно, иначе мёртвый ограничитель невидим — он
// навешен, исполняется на каждом запросе и не отверг ни разу, ровно как и тот,
// чей ключ всегда пуст. Отличает их пара чисел: ноль отвергнутых при нуле
// допущенных означает «никто не звал», а при миллионе допущенных — «предел ни
// разу не достигнут».
const admissionReportInterval = time.Minute

// admissionIdleWindow — за сколько простоя ведро субъекта убирается. Величина
// щедрая намеренно: убрать ведро значит вернуть субъекту полный всплеск, поэтому
// уборка обязана отставать от окна, в котором предел ещё имеет смысл.
const admissionIdleWindow = 10 * time.Minute

// listenerAdmission — пара ограничителей процесса. Ноль означает «величины не
// объявлены»; такое состояние возможно ТОЛЬКО вне боевого режима — боевую посадку
// с необъявленными величинами не поднимает страж старта S7.
type listenerAdmission struct {
	public   *grpcsrv.Admission
	internal *grpcsrv.Admission
}

// buildAdmission собирает ограничители из объявленных посадкой величин.
//
// Величины читаются ТЕМИ ЖЕ методами, которые спрашивает страж старта
// (config.Config.PublicAdmissionLimits / InternalAdmissionLimits), поэтому
// «страж пропустил» ⟺ «листенер ограничен» — по построению, а не по совпадению
// двух одинаково написанных условий.
//
// Необъявленные величины дают НОЛЬ ограничителей, а не пустой: объект, который
// выглядит ограничителем и не ограничивает, — ровно тот класс, который мы ловим
// в чужом коде. Вне боевого режима это законно (внутрипроцессные фикстуры), и
// журнал говорит об этом прямо.
func buildAdmission(cfg config.Config, logger *slog.Logger) (listenerAdmission, error) {
	var out listenerAdmission

	build := func(name string, limits grpcsrv.AdmissionLimits, subject grpcsrv.SubjectFunc) (*grpcsrv.Admission, error) {
		if !limits.IsDeclared() {
			logger.Warn("request admission limits are not declared for this listener; "+
				"the listener accepts an unbounded request rate from a single caller",
				"listener", name,
				"knob", "api-server.rate-limit."+name,
				"note", "production mode refuses to start in this state (boot guard S7)")
			return nil, nil
		}
		a, err := grpcsrv.NewAdmission(name, limits, subject)
		if err != nil {
			return nil, fmt.Errorf("ограничитель допуска листенера %s: %w", name, err)
		}
		logger.Info("request admission armed", "listener", name, "limits", limits.String())
		return a, nil
	}

	var err error
	if out.public, err = build("public", cfg.PublicAdmissionLimits(), grpcsrv.PrincipalSubject); err != nil {
		return listenerAdmission{}, err
	}
	if out.internal, err = build("internal", cfg.InternalAdmissionLimits(), grpcsrv.CertIdentitySubject); err != nil {
		return listenerAdmission{}, err
	}
	return out, nil
}

// guard оборачивает регистратор ограничителем, если он собран.
//
// Пустой ограничитель не подставляется даже пустышкой: обёртка-пропускалка
// выглядела бы в трассировке так же, как настоящая, и «ограничителя нет» стало бы
// неотличимо от «ограничитель ничего не отверг».
func guard(a *grpcsrv.Admission, reg grpc.ServiceRegistrar) grpc.ServiceRegistrar {
	if a == nil {
		return reg
	}
	return a.Registrar(reg)
}

// report — задача носителя: периодически печатает счётчики обоих листенеров и
// убирает вёдра простаивающих субъектов.
//
// Уборка живёт здесь, а не в самом ограничителе: пространство личностей задаёт
// вызывающий, поэтому у роста числа вёдер обязан быть владелец в композиционном
// корне, а не фоновая горутина, запущенная библиотекой за спиной у процесса.
// Собственный жёсткий потолок у ограничителя при этом остаётся — уборка его
// дополняет, а не заменяет.
func (l listenerAdmission) report(ctx context.Context, logger *slog.Logger) {
	if l.public == nil && l.internal == nil {
		return
	}
	t := time.NewTicker(admissionReportInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			l.logOnce(logger, "request admission final counters")
			return
		case <-t.C:
			for _, a := range []*grpcsrv.Admission{l.public, l.internal} {
				if a != nil {
					a.EvictIdle(admissionIdleWindow)
				}
			}
			l.logOnce(logger, "request admission counters")
		}
	}
}

func (l listenerAdmission) logOnce(logger *slog.Logger, msg string) {
	for _, a := range []*grpcsrv.Admission{l.public, l.internal} {
		if a == nil {
			continue
		}
		s := a.Stats()
		logger.Info(msg,
			"listener", a.Listener(),
			"admitted", s.Admitted,
			"rejected_rate", s.RejectedRate,
			"rejected_in_flight", s.RejectedInFlight,
			"subjects", s.Subjects)
	}
}
