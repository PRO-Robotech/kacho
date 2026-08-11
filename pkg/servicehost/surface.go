// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package servicehost

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// ServeSurface поднимает НЕ-gRPC поверхность, обслуживает её до отмены ctx и
// возвращается ТОЛЬКО ПОСЛЕ ТОГО, как порт освобождён.
//
// # Профиль той же функции: что здесь то же, а что другое
//
// То же: объявление приносится ДАННЫМИ ([servicecontract.SurfaceDescriptor]),
// отказы отработали до первого принятого соединения, вызывающему не остаётся
// объекта, к которому можно приделать своё. Другое: цепочки звеньев здесь нет —
// у HTTP-поверхности своя аутентификация внутри обработчика либо объявленное её
// отсутствие, и подмешивать сюда gRPC-цепочку было бы не унификацией, а
// прочтением намеренно узкой семантики за общий случай.
//
// # Почему функция БЛОКИРУЮЩАЯ, а не пара «запусти» + «погаси»
//
// Пара возвращала вызывающему обязанность позвать гашение, и обязанность эта
// разошлась: из шести диагностических слушателей дерева двое гасились
// контекстом БЕЗ СРОКА (то есть ждали последний скрейп неограниченно), двое —
// сроком дренажа исполнителей операций, а плоскость данных реестра
// поднималась вовсе мимо синхронной привязки порта — ошибка занятого адреса там
// попадала в журнал, и процесс продолжал работать, объявляя себя исправным.
//
// Здесь гашение — не обязанность вызывающего, а СВОЙСТВО функции: слушатель
// живёт ровно столько, сколько живёт ctx, и пережить его не может, потому что
// сервер вызывающему не отдан. Слушатель, переживший отмену, держит порт — и
// следующий старт того же процесса спотыкается о собственный предыдущий.
//
// # Порядок гашения, и почему он именно такой
//
//  1. отмена ctx;
//  2. `Shutdown` со СВЕЖИМ контекстом на объявленный срок — свежим потому, что
//     ctx уже отменён, и передать его сюда значило бы отменить гашение в тот же
//     миг, в который оно началось (запросы в полёте оборвались бы);
//  3. по истечении срока — `Close`: гашение не бывает бесконечным, и ждать
//     дольше объявленного значит держать порт вопреки объявлению.
//
// Возврат происходит после шага 3, а не после шага 1: вызывающий, дождавшийся
// этой функции, знает, что порт свободен.
func ServeSurface(ctx context.Context, d servicecontract.SurfaceDescriptor) error {
	if !d.Accepted() {
		return errors.New("servicehost: профиль поверхности не принят конструктором " +
			"(servicecontract.NewSurface). Нулевое значение собирается литералом и не проходило " +
			"ни одного отказа — поднимать по нему слушатель значит не проверить ничего")
	}
	spec := d.Spec()
	log := spec.Logger.With(
		"service", string(spec.Service),
		"surface", string(spec.Name))

	// Выключено — ОБЪЯВЛЕНИЕМ, поэтому и в журнале это объявление, а не тишина.
	// Молчаливый пропуск возвращал бы неразличимость «выключено намеренно» и
	// «профиль развёртывания забыл эндпоинт», ради устранения которой ось адреса
	// и заведена.
	if !d.Enabled() {
		log.Warn("servicehost: поверхность выключена объявлением",
			"because", d.DisabledBecause(),
			"mode", spec.Mode.String())
		return nil
	}

	addr, _ := spec.Addr.Get()
	srv := &http.Server{
		Handler:           spec.Handler,
		ReadHeaderTimeout: spec.ReadHeaderBudget,
		IdleTimeout:       spec.IdleBudget,
	}
	// Потолок запроса ставится ТОЛЬКО когда объявлен величиной: у потоковой
	// поверхности его нет намеренно, и подставить сюда «что-нибудь» значило бы
	// разорвать исправную передачу слоя образа по таймеру.
	if budget, ok := spec.RequestBudget.Get(); ok {
		srv.ReadTimeout = budget
		srv.WriteTimeout = budget
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("servicehost: %s не поднимает поверхность %q на %s: %w",
			spec.Service, spec.Name, addr, err)
	}
	if spec.TLS != nil {
		lis = tls.NewListener(lis, spec.TLS)
	}

	// Самоотчёт печатается ВСЕГДА и несёт решение об аутентификации ОДНОЙ
	// строкой. До профиля этой строки не было ни у одного из одиннадцати
	// слушателей: журнал сообщал адрес и пути, а «кто сюда вправе постучаться»
	// не сообщал никто — то есть снятая аутентификация была неотличима от
	// забытой не только в коде, но и на живом стенде.
	log.Info("servicehost: поверхность поднята",
		"addr", addr,
		"reach", spec.Reach.String(),
		"auth", d.AuthStatement(),
		"tls", spec.TLS != nil,
		"mode", spec.Mode.String(),
		"read_header_budget", spec.ReadHeaderBudget.String(),
		"idle_budget", spec.IdleBudget.String(),
		"shutdown_budget", spec.ShutdownBudget.String())

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		// Свежий контекст: ctx уже отменён, и гашение по нему истекло бы, не
		// начавшись.
		sctx, cancel := context.WithTimeout(context.Background(), spec.ShutdownBudget)
		defer cancel()
		if serr := srv.Shutdown(sctx); serr != nil {
			log.Warn("servicehost: поверхность не догасилась в объявленный срок; закрываю",
				"budget", spec.ShutdownBudget.String(), "err", serr)
			_ = srv.Close()
		}
	}()

	serveErr := srv.Serve(lis)
	<-stopped
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("servicehost: поверхность %q сервиса %s остановлена: %w",
			spec.Name, spec.Service, serveErr)
	}
	log.Info("servicehost: поверхность погашена, порт освобождён", "addr", addr)
	return nil
}
