// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package servicehost

// admission.go — потолок темпа и одновременности на вызывающего, провязанный
// НОСИТЕЛЕМ, а не каждым сервисом отдельно.
//
// # Почему здесь, а не в композиционном корне сервиса
//
// Механизм жил в фундаменте и был провязан у ОДНОГО места сборки сервера из
// десяти — при том, что заметить это можно было только сплошной переписью.
// Пока проводка принадлежала корню, «провязал» и «не провязал» выглядели
// одинаково: сервис поднимался, отчитывался о посадке и обслуживал
// неограниченный поток одного арендатора. Здесь у неё один владелец, и потеря
// провязки у одного из шести перестаёт быть возможной по построению — регистрация
// идёт ЧЕРЕЗ обёртку, а другого пути зарегистрировать службу у вызывающего нет.
//
// # Почему обёртка регистратора, а не звено цепочки
//
// Обёртка получает дескриптор службы целиком и подставляет допуск МЕЖДУ цепочкой
// и обработчиком. Место выбрано не из удобства: ключом служит личность, которую
// устанавливают звенья цепочки (личность сертификата → круг доверенных
// отправителей → личность конечного пользователя). Ограничитель, ключующийся
// раньше этого решения, снимается подстановкой чужого заголовка — то есть
// ограничивает только того, кто не пытается его обойти.
//
// Служебные поверхности (проверка здоровья, отражение) регистрируются
// конструктором сервера ДО обёртки и под предел не попадают намеренно: отказ
// проверке готовности означал бы перезапуск пода из-за нагрузки на API.
//
// # Почему у слушателей РАЗНЫЕ ключи
//
//   - публичный — по личности конечного пользователя: за краем сидит арендатор,
//     и предел объявлен на него;
//   - внутренний — по личности СЕРТИФИКАТА: его зовут наши модули, и запрос
//     одного модуля несёт личности разных арендаторов. Ключ по арендатору дробил
//     бы бюджет соседа на тысячу вёдер и душил бы его на ровном месте.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// admissionReportInterval — как часто в журнал уходит счёт допущенных и
// отвергнутых.
//
// Отчёт печатается ВСЕГДА, а не только при находках: «ноль отказов за всю жизнь
// контроля» обязано быть заметно, иначе мёртвый ограничитель невидим — он
// навешен, исполняется на каждом запросе и не отверг ни разу, ровно как и тот,
// чей ключ всегда пуст. Отличает их ПАРА чисел: ноль отвергнутых при нуле
// допущенных означает «никто не звал», а при миллионе допущенных — «предел ни
// разу не достигнут», и это разные факты.
const admissionReportInterval = time.Minute

// admissionIdleWindow — за сколько простоя ведро субъекта убирается. Величина
// щедрая намеренно: убрать ведро значит вернуть субъекту полный всплеск, поэтому
// уборка обязана отставать от окна, в котором предел ещё имеет смысл.
const admissionIdleWindow = 10 * time.Minute

// admission — пара ограничителей процесса. Ноль означает объявленное изъятие;
// на боевой посадке такое объявление до сюда не доезжает — его отвергает
// конструктор дескриптора.
type admission struct {
	public   *grpcsrv.Admission
	internal *grpcsrv.Admission
}

// buildAdmission собирает ограничители из объявленной оси дескриптора.
//
// Негодный набор — ОТКАЗ, а не пустой ограничитель: объект, который выглядит
// ограничителем и не ограничивает, есть ровно тот класс, который мы ловим в
// чужом коде. Дескриптор такой набор уже отверг бы, поэтому отказ здесь —
// вторая линия и он назван: молча отдать ноль значило бы превратить ошибку
// сборки в тихое снятие защиты.
func buildAdmission(spec servicecontract.Spec) (admission, error) {
	limits, declared := spec.Admission.Get()
	if !declared {
		return admission{}, nil
	}
	var (
		out admission
		err error
	)
	if out.public, err = grpcsrv.NewAdmission("public", limits.Public, grpcsrv.PrincipalSubject); err != nil {
		return admission{}, fmt.Errorf("servicehost: ограничитель допуска публичного слушателя: %w", err)
	}
	if out.internal, err = grpcsrv.NewAdmission("internal", limits.Internal, grpcsrv.CertIdentitySubject); err != nil {
		return admission{}, fmt.Errorf("servicehost: ограничитель допуска внутреннего слушателя: %w", err)
	}
	return out, nil
}

// guardedBy оборачивает регистратор ограничителем, если он собран.
//
// Пустой ограничитель не подставляется даже пустышкой: обёртка-пропускалка
// выглядела бы в трассировке так же, как настоящая, и «ограничителя нет» стало
// бы неотличимо от «ограничитель ничего не отверг».
func guardedBy(a *grpcsrv.Admission, reg grpc.ServiceRegistrar) grpc.ServiceRegistrar {
	if a == nil {
		return reg
	}
	return a.Registrar(reg)
}

// handOut отдаёт регистраторам ОБА слушателя — каждый под своим ограничителем.
//
// Функция существует затем, чтобы «через обёртку» было ОДНИМ местом, а не двумя
// строками, повторёнными в [Serve]: две строки разъехались бы на первой же
// правке, и разъехались бы молча — сервис, чей внутренний слушатель потерял
// обёртку, поднимается и отчитывается ровно так же.
func (a admission) handOut(publicSrv, internalSrv grpc.ServiceRegistrar, public, internal Registrar) {
	public(guardedBy(a.public, publicSrv))
	internal(guardedBy(a.internal, internalSrv))
}

// arm печатает объявленные величины ОБОИХ слушателей.
//
// Печатается и тогда, когда ограничителя нет: «потолка нет» обязано быть видно
// в журнале посадки, а не выводиться из отсутствия строки. Отсутствие строки
// читается как «версия старая» ничуть не реже, чем как «потолка нет».
func (a admission) arm(log *slog.Logger, service servicecontract.ServiceName, because string) {
	for _, l := range []*grpcsrv.Admission{a.public, a.internal} {
		if l == nil {
			continue
		}
		log.Info("servicehost: request admission armed",
			"service", string(service), "listener", l.Listener(), "limits", l.Limits().String())
	}
	if a.public == nil && a.internal == nil {
		log.Warn("servicehost: request admission is NOT armed on either listener; "+
			"a single caller may take an unbounded request rate",
			"service", string(service), "because", because)
	}
}

// report — фоновая задача носителя: периодически печатает счётчики обоих
// слушателей и убирает вёдра простаивающих субъектов.
//
// Уборка живёт здесь, а не внутри самого ограничителя: пространство личностей
// задаёт вызывающий, поэтому у роста числа вёдер обязан быть владелец в
// носителе, а не фоновая горутина, запущенная библиотекой за спиной у процесса.
// Собственный жёсткий потолок у ограничителя при этом остаётся — уборка его
// дополняет, а не заменяет.
func (a admission) report(ctx context.Context, log *slog.Logger) {
	if a.public == nil && a.internal == nil {
		return
	}
	t := time.NewTicker(admissionReportInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			a.logOnce(log, "servicehost: request admission final counters")
			return
		case <-t.C:
			for _, l := range []*grpcsrv.Admission{a.public, a.internal} {
				if l != nil {
					l.EvictIdle(admissionIdleWindow)
				}
			}
			a.logOnce(log, "servicehost: request admission counters")
		}
	}
}

func (a admission) logOnce(log *slog.Logger, msg string) {
	for _, l := range []*grpcsrv.Admission{a.public, a.internal} {
		if l == nil {
			continue
		}
		s := l.Stats()
		log.Info(msg,
			"listener", l.Listener(),
			"admitted", s.Admitted,
			"rejected_rate", s.RejectedRate,
			"rejected_in_flight", s.RejectedInFlight,
			"subjects", s.Subjects)
	}
}
