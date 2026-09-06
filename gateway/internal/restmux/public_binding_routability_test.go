// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// public_binding_routability_test.go — каждый публичный REST-биндинг контракта
// имеет маршрут на внешнем листенере, и предмет проверки ВЫЧИСЛЯЕТСЯ из
// дескрипторов, а не перечисляется руками.
//
// ЗАЧЕМ. Про REST принято было говорить, что он «выводится из дескрипторов и
// отстать не может». Это верно ПОМЕТОДНО и неверно ПОСЕРВИСНО: методы внутри
// сервиса действительно берутся из дескрипторов, но САМА регистрация — рукописная
// строка `Register<X>ServiceHandlerFromEndpoint`, по одной на сервис, и часть из
// них ещё и условная (домен регистрируется, только если его адрес непуст —
// осознанная уступка тому, что backend может быть не задеплоен). Публичный
// сервис без такой строки либо с пустым адресом не имеет ни одного маршрута.
//
// ПОЧЕМУ ЭТО НЕ ВИДНО. Отсутствие маршрута отвечает 404 — тем же кодом, которым
// диспетчер НАМЕРЕННО укрывает административные пути, пришедшие на внешний
// листенер (заголовок mux.go: «Запрос, классифицированный как internal, но
// пришедший на ВНЕШНИЙ листенер, получает 404 (existence-hiding)»). Забытая или
// отключённая регистрация неотличима от намеренного сокрытия — ровно та же
// неразличимость, что и на gRPC-полосе.
//
// ЧТО БЫЛО ВМЕСТО ЭТОГО. Пробы «маршруты домена зарегистрированы» — подоменные и
// рукописные (nlb, storage, registry, geo, redesign), каждая со своим списком
// путей и своей картой адресов, собранной внутри самой пробы. Ни одна не считала
// разность против дескрипторов и ни одна не пользовалась картой, которую
// передаёт composition root.
//
// ДИСКРИМИНАТОР НЕ ПОСТУЛИРУЕТСЯ. «404 ⟺ маршрута нет» подтверждается встречной
// пробой ниже: та же проба на карте без домена обязана показать ВСЕ его биндинги
// отсутствующими и НЕ задеть ни одного чужого.
package restmux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/contractroot"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// minPublicBindings — пол переписи: не «примерно про размер контракта», а про то,
// что таблица биндингов вообще прочиталась. Пустой реестр даёт ноль находок так
// же убедительно, как полное совпадение.
const minPublicBindings = 200

// probeAddrs берёт карту адресов У COMPOSITION ROOT (main.go передаёт в NewMux
// ровно `cfg.BackendAddrs()`) и заменяет непустые значения на заведомо мёртвый
// литерал, сохраняя пустоту там, где она есть.
//
// Подменяется ровно то, что на предмет пробы не влияет, и сохраняется ровно то,
// что влияет: регистрация условных доменов гейтится ПУСТОТОЙ адреса
// (`if storageAddr != ""`), а не его значением. Умолчания — DNS-имена сервисов
// кластера; вне кластера каждый пробный запрос ждёт их разрешения, и проба
// занимала 43 секунды вместо долей секунды, измеряя резолвер DNS, а не маршруты.
func probeAddrs(t *testing.T) map[string]string {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	src := cfg.BackendAddrs()
	if len(src) == 0 {
		t.Fatal("карта адресов composition root'а пуста — предмета у пробы нет")
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		if v == "" {
			out[k] = ""
			continue
		}
		out[k] = "127.0.0.1:1"
	}
	return out
}

// publicBinding — один публичный REST-биндинг, приведённый к конкретному пути.
type publicBinding struct {
	method, path, fqn string
}

func (b publicBinding) service() string { return b.fqn[:strings.IndexByte(b.fqn, '/')] }

// probePath подставляет конкретный сегмент вместо каждой переменной шаблона:
// "/vpc/v1/subnets/{subnet_id}:addCidrBlocks" → "/vpc/v1/subnets/probeid:addCidrBlocks".
// Суффикс-глагол после "}" сохраняется — он часть маршрута, а не переменной.
func probePath(tmpl string) string {
	var b strings.Builder
	for _, seg := range strings.Split(strings.TrimPrefix(tmpl, "/"), "/") {
		b.WriteByte('/')
		open := strings.IndexByte(seg, '{')
		if open < 0 {
			b.WriteString(seg)
			continue
		}
		end := strings.IndexByte(seg[open:], '}')
		if end < 0 {
			b.WriteString(seg[:open] + "probeid")
			continue
		}
		b.WriteString(seg[:open] + "probeid" + seg[open+end+1:])
	}
	return b.String()
}

// publicBindingsFromDescriptors — предмет гейта, вычисленный из контракта.
func publicBindingsFromDescriptors() []publicBinding {
	var out []publicBinding
	for _, b := range loadedHTTPBindings() {
		if b.internal || !underDeclaredRoot(b.fqn) {
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

// unrouted прогоняет предмет через диспетчер на ВНЕШНЕМ происхождении (маркера
// нет — внешнее это fail-closed умолчание) и возвращает биндинги без маршрута.
func unrouted(t *testing.T, addrs map[string]string, subject []publicBinding) []publicBinding {
	t.Helper()
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	var missing []publicBinding
	for _, b := range subject {
		req := httptest.NewRequest(b.method, b.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			missing = append(missing, b)
		}
	}
	return missing
}

// TestEveryPublicHTTPBindingIsRoutedOnTheExternalListener — положительная сторона.
func TestEveryPublicHTTPBindingIsRoutedOnTheExternalListener(t *testing.T) {
	addrs := probeAddrs(t)
	subject := publicBindingsFromDescriptors()
	if len(subject) < minPublicBindings {
		t.Fatalf("публичных REST-биндингов в дескрипторах %d (< %d) — реестр пуст или домены "+
			"не слинкованы; пока это не починено, зелёное ниже беспредметно",
			len(subject), minPublicBindings)
	}

	svcs := map[string]struct{}{}
	for _, b := range subject {
		svcs[b.service()] = struct{}{}
	}
	t.Logf("перепись: %d публичных REST-биндингов в %d сервисах, %d ключей в карте адресов",
		len(subject), len(svcs), len(addrs))

	missing := unrouted(t, addrs, subject)
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
	t.Errorf("%d публичных REST-биндингов из %d не имеют маршрута на внешнем листенере — "+
		"сервис не зарегистрирован в mux.go либо у его домена пустой адрес. Вызывающий получает "+
		"404, то есть ровно то, чем диспетчер намеренно укрывает административные пути: "+
		"недоделка неотличима от сокрытия.\n%s\nпервый: %s %s (%s)",
		len(missing), len(subject), strings.Join(lines, "\n"),
		missing[0].method, missing[0].path, missing[0].fqn)
}

// TestPublicBindingProbe_SeesAMissingRouteAsMissing — отрицание в паре с
// положительным: та же проба, тот же предмет, карта адресов без одного домена.
//
// Домен взят из числа тех, чья регистрация УСЛОВНА по адресу — только на таком
// пустой адрес и производит отсутствие маршрута. Для безусловно регистрируемых
// доменов (vpc/compute/iam) пустой адрес маршрут не убирает: он остаётся и
// падает при обращении. Проба, выбравшая безусловный домен, объявила бы «предмет
// не воспроизводится» на исправном коде — и это уже было: первая редакция брала
// compute и краснела на пустом месте.
func TestPublicBindingProbe_SeesAMissingRouteAsMissing(t *testing.T) {
	const dropped = "loadbalancer"

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

	subject := publicBindingsFromDescriptors()
	var ofDropped, untouched []publicBinding
	for _, b := range subject {
		if belongsToDomain(b.fqn, dropped) {
			ofDropped = append(ofDropped, b)
		} else {
			untouched = append(untouched, b)
		}
	}
	if len(ofDropped) == 0 {
		t.Fatalf("в контракте нет публичных биндингов домена %q — контроль потерял предмет", dropped)
	}

	if missing := unrouted(t, stripped, ofDropped); len(missing) != len(ofDropped) {
		t.Fatalf("без адреса домена %q проба нашла %d биндингов без маршрута из %d — она не "+
			"различает зарегистрированный маршрут от незарегистрированного, значит её зелёное "+
			"в соседней пробе ничего не значит", dropped, len(missing), len(ofDropped))
	}

	// Законный близнец: домены, которых мы не трогали, остаются
	// отмаршрутизированными на той же урезанной карте — иначе проба краснеет на
	// чём угодно, и её красное значит не больше её зелёного.
	if collateral := unrouted(t, stripped, untouched); len(collateral) > 0 {
		t.Fatalf("удаление домена %q утащило за собой %d чужих биндингов (первый — %s %s): "+
			"проба реагирует не на предмет", dropped, len(collateral),
			collateral[0].method, collateral[0].path)
	}
	t.Logf("контроль: %d биндингов домена %q исчезают без его адреса, %d чужих не задеты",
		len(ofDropped), dropped, len(untouched))
}

// belongsToDomain — принадлежит ли полное имя домену, под КАКИМ БЫ корнем тот ни
// лежал. Литерал одного корня не узнал бы домена второго, и проба «выброшенный
// домен исчезает из таблицы» зеленела бы, ничего не выбросив.
func belongsToDomain(fqn, domain string) bool {
	for _, root := range contractroot.Roots {
		if strings.HasPrefix(fqn, root+".cloud."+domain+".") {
			return true
		}
	}
	return false
}
