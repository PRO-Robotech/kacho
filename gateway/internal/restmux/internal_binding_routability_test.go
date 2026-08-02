// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// internal_binding_routability_test.go — каждый АДМИНИСТРАТИВНЫЙ REST-биндинг
// контракта имеет маршрут на ВНУТРЕННЕМ листенере, и предмет проверки
// ВЫЧИСЛЯЕТСЯ из дескрипторов.
//
// РАДИУС. Соседний гейт (public_binding_routability_test.go) закрыл ту же форму
// на ВНЕШНЕМ листенере и явно отбрасывает `b.internal`. Административная
// половина той же таблицы биндингов осталась без вычисляемой проверки, хотя
// механика отказа у неё та же самая: регистрация — рукописная строка
// `Register<X>ServiceHandlerFromEndpoint`, УСЛОВНАЯ по непустоте адреса домена
// (`if storageInternalAddr != ""`), а отсутствие маршрута отвечает 404.
//
// ПЕРЕПИСЬ, КОТОРАЯ ЭТО УСТАНОВИЛА (ревизия 89242e6b, предикат — весь
// `go test ./gateway/...`, единица — заявление регистрации): из ЧЕТЫРНАДЦАТИ
// регистраций Internal*-сервисов в mux.go удаление ТРЁХ не замечает ни один тест
// дерева, прогон остаётся rc=0 — `InternalNetworkService` (vpc),
// `InternalDiskTypeService` (storage) и `InternalResourceLifecycleService` (nlb,
// pro-forma: http-аннотаций у него нет). В методах это 4 из 34 административных
// биндингов. Остальные одиннадцать держат ПОДОМЕННЫЕ РУКОПИСНЫЕ списки путей
// (`internalRESTPaths`, `TestRedesign_InternalRoutes_InternalListenerServes`,
// `TestGeo_S5_AdminCRUDRoutesRegistered_InternalListener`, …) — они защищают
// ровно те строки, которые в них внесены, и не защищают ни новый сервис, ни
// новый биндинг уже покрытого сервиса.
//
// ПОЧЕМУ ЭТО НЕ ВИДНО ИНАЧЕ. Административная поверхность и так недостижима
// снаружи, поэтому «её нет» не производит никакого симптома у тенанта. Заметить
// может только тот, кто ею пользуется — консоль или дежурный, — и уже в
// инциденте. Класс не гипотетический: iam ConditionsService был в дескрипторах,
// в таблице маршрутов и в каталоге прав и не был смонтирован; шесть REST-путей
// отвечали 404, пока это не починили руками (a96dbe1a), без гейта.
package restmux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// minInternalBindings — пол переписи: не «про размер админ-поверхности», а про
// то, что таблица биндингов вообще прочиталась и её административная половина не
// пуста. Пустой предмет даёт ноль находок так же убедительно, как полное
// совпадение.
const minInternalBindings = 25

// internalBindingsFromDescriptors — предмет гейта, вычисленный из контракта.
func internalBindingsFromDescriptors() []publicBinding {
	var out []publicBinding
	for _, b := range loadedHTTPBindings() {
		if !b.internal || !strings.HasPrefix(b.fqn, "kacho.cloud.") {
			continue
		}
		out = append(out, publicBinding{method: b.method, path: probePath(b.template), fqn: b.fqn})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].fqn != out[j].fqn {
			return out[i].fqn < out[j].fqn
		}
		return out[i].path < out[j].path
	})
	return out
}

// routingRefusal — «у диспетчера НЕТ такого маршрута».
//
// Дискриминатор не постулируется. Адреса бэкендов подменены заведомо мёртвым
// литералом, поэтому НАЙДЕННЫЙ маршрут доходит до dial и отвечает статусом,
// выведенным из gRPC-отказа (`Unavailable` → 503). Отсутствующий отвечает
// маршрутной ошибкой grpc-gateway: 404, когда пути нет вовсе, и 501, когда путь
// есть под другим методом — второе возникает у admin-CRUD каталога DiskType,
// который делит collection-путь с публичным чтением. Обе формы — про маршрут,
// поэтому обе считаются отсутствием; проверка «хоть один субъект отвечает
// НЕ-маршрутной ошибкой» ниже не даёт этому предикату стать всеядным.
func routingRefusal(code int) bool {
	return code == http.StatusNotFound || code == http.StatusNotImplemented
}

// servedOnInternalOrigin прогоняет предмет через диспетчер на ВНУТРЕННЕМ
// происхождении и возвращает биндинги, которым диспетчер ответил отсутствием
// маршрута.
func servedOnInternalOrigin(t *testing.T, addrs map[string]string, subject []publicBinding) (missing []publicBinding, routed int) {
	t.Helper()
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	for _, b := range subject {
		req := httptest.NewRequest(b.method, b.path, nil)
		req = req.WithContext(listenerorigin.WithInternal(req.Context()))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if routingRefusal(rec.Code) {
			missing = append(missing, b)
			continue
		}
		routed++
	}
	return missing, routed
}

// TestEveryInternalHTTPBindingIsRoutedOnTheInternalListener — положительная сторона.
func TestEveryInternalHTTPBindingIsRoutedOnTheInternalListener(t *testing.T) {
	addrs := probeAddrs(t)
	subject := internalBindingsFromDescriptors()
	if len(subject) < minInternalBindings {
		t.Fatalf("административных REST-биндингов в дескрипторах %d (< %d) — реестр пуст или "+
			"Internal*-домены не слинкованы; пока это не починено, зелёное ниже беспредметно",
			len(subject), minInternalBindings)
	}

	svcs := map[string]struct{}{}
	for _, b := range subject {
		svcs[b.service()] = struct{}{}
	}
	t.Logf("перепись: %d административных REST-биндингов в %d сервисах, %d ключей в карте адресов",
		len(subject), len(svcs), len(addrs))

	missing, routed := servedOnInternalOrigin(t, addrs, subject)
	if routed == 0 {
		t.Fatalf("ни один из %d субъектов не ответил НЕ-маршрутной ошибкой — значит предикат "+
			"routingRefusal сейчас истинен на всём, и его ноль находок ничего не значил бы",
			len(subject))
	}
	if len(missing) == 0 {
		return
	}
	perSvc := map[string]int{}
	for _, b := range missing {
		perSvc[b.service()]++
	}
	names := make([]string, 0, len(perSvc))
	for s := range perSvc {
		names = append(names, s)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, s := range names {
		lines = append(lines, "  "+s+": "+strconv.Itoa(perSvc[s])+" биндингов без маршрута")
	}
	t.Errorf("%d административных REST-биндингов из %d не имеют маршрута на ВНУТРЕННЕМ листенере — "+
		"сервис не зарегистрирован в mux.go либо у его домена пустой адрес. Снаружи это не "+
		"производит никакого симптома (админ-поверхность там и так укрыта), поэтому заметит только "+
		"тот, кто ею пользуется, и уже в инциденте.\n%s\nпервый: %s %s (%s)",
		len(missing), len(subject), strings.Join(lines, "\n"),
		missing[0].method, missing[0].path, missing[0].fqn)
}

// TestInternalBindingProbe_SeesAMissingRouteAsMissing — отрицание в паре с
// положительным: тот же предмет, карта адресов без одного административного
// домена.
//
// Домен взят из числа тех, чья регистрация условна по адресу, — только на таком
// пустой адрес и производит отсутствие маршрута. Проба, выбравшая безусловно
// регистрируемый домен, объявила бы «предмет не воспроизводится» на исправном
// коде.
func TestInternalBindingProbe_SeesAMissingRouteAsMissing(t *testing.T) {
	const dropped = "storageInternal"
	const droppedPkg = "kacho.cloud.storage.v1."

	full := probeAddrs(t)
	if full[dropped] == "" {
		t.Fatalf("домен %q и так без адреса — контроль потерял предмет и перестал что-либо "+
			"доказывать", dropped)
	}
	stripped := make(map[string]string, len(full))
	for k, v := range full {
		if k != dropped {
			stripped[k] = v
		}
	}

	subject := internalBindingsFromDescriptors()
	var ofDropped, untouched []publicBinding
	for _, b := range subject {
		if strings.HasPrefix(b.fqn, droppedPkg) {
			ofDropped = append(ofDropped, b)
		} else {
			untouched = append(untouched, b)
		}
	}
	if len(ofDropped) == 0 {
		t.Fatalf("в контракте нет административных биндингов домена %q — контроль потерял предмет", dropped)
	}

	if missing, _ := servedOnInternalOrigin(t, stripped, ofDropped); len(missing) != len(ofDropped) {
		t.Fatalf("без адреса домена %q проба нашла %d биндингов без маршрута из %d — она не "+
			"различает зарегистрированный маршрут от незарегистрированного, значит её зелёное "+
			"в соседней пробе ничего не значит", dropped, len(missing), len(ofDropped))
	}

	// Законный близнец: домены, которых мы не трогали, остаются
	// отмаршрутизированными на той же урезанной карте — иначе проба краснеет на
	// чём угодно, и её красное значит не больше её зелёного.
	if collateral, _ := servedOnInternalOrigin(t, stripped, untouched); len(collateral) > 0 {
		t.Fatalf("удаление домена %q утащило за собой %d чужих биндингов (первый — %s %s): "+
			"проба реагирует не на предмет", dropped, len(collateral),
			collateral[0].method, collateral[0].path)
	}
	t.Logf("контроль: %d биндингов домена %q исчезают без его адреса, %d чужих не задеты",
		len(ofDropped), dropped, len(untouched))
}
