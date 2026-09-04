// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"strings"
	"sync"

	"google.golang.org/genproto/googleapis/api/annotations"
)

// Классификация «этот REST-запрос — внутренний» строится из proto-дескрипторов,
// слинкованных в бинарь gateway, а не из списка строк, поддерживаемого руками.
//
// Почему именно так. Часть Internal*-биндингов делит REST-путь с ПУБЛИЧНЫМ
// чтением того же ресурса и отличается только HTTP-методом: каталог DiskType
// (storage и compute) отдаёт `GET /<domain>/v1/diskTypes[/{id}]` публичным
// DiskTypeService, а `POST`/`PATCH`/`DELETE` по тем же путям обслуживает
// admin-only InternalDiskTypeService на :9091. Предикат, смотрящий на одну лишь
// строку пути, такие пары не различает в принципе — решение обязано быть keyed
// на пару (метод, путь).
//
// Ручной список к тому же дрейфует: он покрывал только те Internal-пути, что
// несут сегмент `/internal` или живут в vpc, и молча пропускал admin-CRUD
// каталога дисков в ДВУХ доменах. Дескрипторы — единственный источник, который
// не может разойтись с proto: новый Internal-RPC с REST-аннотацией попадает
// сюда автоматически.

// internalRouteSegment — один сегмент шаблона пути из `google.api.http`.
// Литеральный сегмент несёт только prefix; сегмент-переменная (`{id}`) несёт
// wild=true плюс литеральные prefix/suffix вокруг скобок (так выражается
// verb-суффикс вида `{network_id}:internal`); `{x=**}` даёт rest=true и
// поглощает все оставшиеся сегменты.
type internalRouteSegment struct {
	prefix string
	suffix string
	wild   bool
	rest   bool
}

// internalRoute — один REST-биндинг Internal*-сервиса.
type internalRoute struct {
	method string
	segs   []internalRouteSegment
}

var (
	internalRoutesOnce sync.Once
	internalRoutes     []internalRoute
)

// loadedInternalRoutes возвращает таблицу внутренних REST-маршрутов, собирая её
// при первом обращении.
func loadedInternalRoutes() []internalRoute {
	internalRoutesOnce.Do(func() { internalRoutes = buildInternalRoutes() })
	return internalRoutes
}

// isInternalServiceName — обе принятые в kacho-proto конвенции именования
// internal-сервисов: префикс `Internal<X>Service` и суффикс `<X>InternalService`.
// Совпадает с предикатом gRPC-роутера (allowlist.HasInternalSuffix), только
// применяется к имени сервиса из дескриптора, а не к строке gRPC-метода.
func isInternalServiceName(name string) bool {
	if strings.HasSuffix(name, "InternalService") {
		return true
	}
	return strings.HasPrefix(name, "Internal") && strings.HasSuffix(name, "Service")
}

// buildInternalRoutes отбирает из полной таблицы REST-биндингов
// (rest_bindings.go, собирается из тех же proto-дескрипторов) те, что
// принадлежат Internal*-сервисам домена kacho.
func buildInternalRoutes() []internalRoute {
	var out []internalRoute
	for _, b := range loadedHTTPBindings() {
		if !b.internal {
			continue
		}
		out = append(out, internalRoute{method: b.method, segs: b.segs})
	}
	return out
}

// httpRuleVerbAndPath раскладывает `google.api.http` в (HTTP-метод, шаблон пути).
func httpRuleVerbAndPath(r *annotations.HttpRule) (string, string) {
	switch p := r.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return "GET", p.Get
	case *annotations.HttpRule_Post:
		return "POST", p.Post
	case *annotations.HttpRule_Put:
		return "PUT", p.Put
	case *annotations.HttpRule_Patch:
		return "PATCH", p.Patch
	case *annotations.HttpRule_Delete:
		return "DELETE", p.Delete
	case *annotations.HttpRule_Custom:
		return strings.ToUpper(p.Custom.GetKind()), p.Custom.GetPath()
	}
	return "", ""
}

// parsePathTemplate разбирает шаблон `google.api.http` в сегменты.
func parsePathTemplate(tmpl string) []internalRouteSegment {
	raw := strings.Split(strings.TrimPrefix(tmpl, "/"), "/")
	segs := make([]internalRouteSegment, 0, len(raw))
	for _, s := range raw {
		open := strings.IndexByte(s, '{')
		closeIdx := strings.LastIndexByte(s, '}')
		if open < 0 || closeIdx < open {
			segs = append(segs, internalRouteSegment{prefix: s})
			continue
		}
		segs = append(segs, internalRouteSegment{
			prefix: s[:open],
			suffix: s[closeIdx+1:],
			wild:   true,
			// `{x=**}` в grpc-gateway поглощает все оставшиеся сегменты.
			rest: strings.Contains(s[open:closeIdx], "**"),
		})
	}
	return segs
}

// matches проверяет, что конкретный запрос попадает в этот биндинг.
func (r internalRoute) matches(method string, segs []string) bool {
	if r.method != method {
		return false
	}
	for i, tmplSeg := range r.segs {
		if tmplSeg.rest {
			return matchesRestOfTemplate(tmplSeg, r.segs[i+1:], segs[min(i, len(segs)):])
		}
		if i >= len(segs) {
			return false
		}
		if !tmplSeg.matchesSegment(segs[i]) {
			return false
		}
	}
	return len(segs) == len(r.segs)
}

// matchesRestOfTemplate — совпадение глубокой подстановки `{x=**}` вместе с ТЕМ,
// что стоит в шаблоне после неё.
//
// `**` поглощает сегменты, но не весь остаток пути: шаблон вправе нести за ней
// литеральный хвост (`{repository=**}/referrers`), а сама подстановка — своё
// окончание (`{repository=**}:rename`). Прежняя редакция отвечала «совпало», как
// только оставался хотя бы один сегмент, и ни хвоста, ни окончания не сверяла —
// поэтому `GET …/repositories/x` совпадал с биндингом `…/referrers`, а
// `POST …/repositories/x` — с биндингом `:rename`.
//
// Правка СУЖАЕТ совпадение до верного и расширить его не может: точное
// совпадение не пропускается ни на одной оси. У классификатора внутренних
// маршрутов предмета этой ошибки не было — глубоких подстановок с хвостом среди
// них ноль (замер печатает `TestHTTPBindingTemplateShapesAreStillPresent`),
// поэтому его вердикт правкой не меняется.
func matchesRestOfTemplate(rest internalRouteSegment, tail []internalRouteSegment, remaining []string) bool {
	// Хвост шаблона обязан совпасть с хвостом пути сегмент в сегмент.
	if len(remaining) < len(tail)+1 { // `**` обязана покрыть хотя бы один сегмент
		return false
	}
	covered := remaining[:len(remaining)-len(tail)]
	for i, ts := range tail {
		if ts.rest {
			// Второй `**` в одном шаблоне grpc-gateway не допускает; форма,
			// которую разбор не описывает, совпадением не считается.
			return false
		}
		if !ts.matchesSegment(remaining[len(covered)+i]) {
			return false
		}
	}
	// Приставка и окончание относятся к покрытому УЧАСТКУ целиком: первая — к
	// его началу, второе — к его концу.
	joined := strings.Join(covered, "/")
	if len(joined) <= len(rest.prefix)+len(rest.suffix) {
		return false
	}
	return strings.HasPrefix(joined, rest.prefix) && strings.HasSuffix(joined, rest.suffix)
}

func (s internalRouteSegment) matchesSegment(actual string) bool {
	if !s.wild {
		return s.prefix == actual
	}
	if len(actual) <= len(s.prefix)+len(s.suffix) {
		// Переменная обязана быть непустой.
		return false
	}
	return strings.HasPrefix(actual, s.prefix) && strings.HasSuffix(actual, s.suffix)
}

// matchesInternalRESTBinding — true, если (метод, путь) совпадает с
// REST-биндингом какого-либо Internal*-сервиса.
func matchesInternalRESTBinding(method, path string) bool {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, r := range loadedInternalRoutes() {
		if r.matches(method, segs) {
			return true
		}
	}
	return false
}
