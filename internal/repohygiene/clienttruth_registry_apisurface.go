// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_registry_apisurface.go — анализатор «публичный глагол объявлен в
// контракте ⇒ он назван на клиентской странице».
//
// # Предмет
//
// Клиентская страница — не пересказ контракта, а ЕДИНСТВЕННЫЙ вход для того, кто
// кода не читает. Глагол, объявленный контрактом и не названный страницей, для
// такого клиента НЕ СУЩЕСТВУЕТ: он строит обходной путь либо уходит, решив, что
// платформа этого не умеет.
//
// Замер на день заведения (#1593): публичная служба registry объявляла ПЯТНАДЦАТЬ
// операций, страницы описывали ДЕВЯТЬ. Среди шести ненайденных — весь жизненный
// цикл репозитория (завести · настроить · снять · переименовать). Цена измерена не
// в неточности: `visibility` репозитория — единственный рычаг публичности образа, а
// единственный признанный страницами путь (`docker push`) заводит репозиторий
// приватным. «Опубликовать образ» по документации было тупиком при реализованной в
// контракте возможности.
//
// # Что судит анализатор
//
// Источник истины — ДЕРЕВО КОНТРАКТОВ: каждый `rpc` службы объявляет операцию.
// Ключ операции берётся из её объявления и нормализуется, потому что две
// поверхности пишут один адрес по-разному:
//
//	контракт   get: "/registry/v1/registries/{registry_id}/repositories/{repository=**}"
//	страница   <ApiOperation method="GET" endpoint="/registry/v1/registries/{registryId}/repositories/{repository}">
//
// Различия — регистр глагола, snake_case против camelCase у имени параметра и
// суффикс `=**` (глубокий шаблон grpc-gateway). Все три снимаются нормализацией
// ОДНОЙ функцией на обе стороны: две функции разошлись бы молча, и расхождение
// пришлось бы ровно на тот адрес, который никто не сверял руками.
//
// RPC без REST-объявления судится по gRPC-форме `Служба/Метод` — так клиентские
// страницы называют внутренние службы. Слепой зоны здесь нет by construction: у
// каждого RPC есть ровно один из двух ключей, и оба разбираются.
//
// # Чего он НЕ судит, и это названо, а не умолчано
//
//  1. **Полноту описания.** Названный на странице глагол может быть описан скверно —
//     анализатор об этом не высказывается. Он ловит УМОЛЧАНИЕ, а не качество: первое
//     машинно решаемо, второе нет.
//  2. **Обратную сторону** — страницу, называющую операцию, которой в контракте нет.
//     Такой класс реален, но его предикат другой (адрес мог принадлежать соседней
//     службе либо общей странице операций), и заводить его надо своей проверкой с
//     собственной инъекцией, а не пристёгивать сюда.
//  3. **Прочие домены.** Анализатор общий, а гейт над ним заведён на registry —
//     единственный домен, чей корпус страниц сверен пообъектно на день заведения.
//     Расширение на соседей — не правка одной строки: у каждого домена свой набор
//     страниц и свои общие адреса, и включать их не глядя значит завести красноту,
//     которую снимут ведомостью исключений.
package repohygiene

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// apiOperationTagRe — тег объявления ЦЕЛИКОМ, без требований к порядку и виду
// атрибутов. Атрибуты извлекаются из его тела отдельно.
//
// Разбор устроен в два шага намеренно. Одно выражение «тег плюс два атрибута в
// таком-то порядке» выглядит проще и заводит слепую зону: замер показал, что мимо
// него уходят ТРИ законные формы записи — обратный порядок атрибутов, одинарные
// кавычки и любой третий атрибут между ними (`method="POST" async endpoint="…"`).
// Записанное такой формой не даёт ни находки, ни зелени — оно просто вне
// наблюдения, и заметить это нечем (testing.md §«Распознаватель обязан знать ВСЕ
// законные формы записи предмета»).
//
// Форма в дереве на день заведения — одна (`method` и `endpoint` в этом порядке, в
// двойных кавычках, 270 вхождений по всем сайтам документации), поэтому расширение
// холостым не выглядит — но оно и не про сегодняшнее дерево, а про то, что первый
// же автор напишет иначе и не узнает об этом.
var apiOperationTagRe = regexp.MustCompile(`(?s)<ApiOperation\b[^>]*>`)

// apiOperationAttrRe — атрибут тега: имя, любые кавычки, значение.
var apiOperationAttrRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*=\s*("([^"]*)"|'([^']*)')`)

// rpcRe — объявление метода службы.
var rpcRe = regexp.MustCompile(`^\s*rpc\s+([A-Za-z0-9_]+)\s*\(`)

// httpRuleRe — REST-объявление метода (`option (google.api.http)`).
var httpRuleRe = regexp.MustCompile(`\b(get|put|post|patch|delete):\s*"([^"]+)"`)

// pathParamRe — сегмент-параметр адреса вместе с возможным шаблоном (`{a_b=**}`).
var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// ContractOperation — операция, объявленная контрактом.
type ContractOperation struct {
	Service string // имя службы (`RegistryService`)
	RPC     string // имя метода (`RenameRepository`)
	Verb    string // `POST` для REST, `gRPC` для метода без REST-объявления
	Address string // нормализованный адрес либо `Служба/Метод`
}

// Key — ключ сверки: глагол и адрес, оба нормализованы.
func (o ContractOperation) Key() string { return o.Verb + " " + o.Address }

// String — как операция называется в находке: человеку нужно имя метода, а не
// только адрес, иначе он не найдёт её в контракте.
func (o ContractOperation) String() string {
	return fmt.Sprintf("%s/%s (%s %s)", o.Service, o.RPC, o.Verb, o.Address)
}

// APISurfaceCensus — объём осмотренного. «Ноль находок» обязано отличаться от
// «ноль прочитанного», поэтому печатается всегда, включая успешный прогон.
type APISurfaceCensus struct {
	Services        int // служб разобрано
	RPCs            int // методов разобрано
	RESTOperations  int // из них с REST-объявлением
	Pages           int // клиентских страниц прочитано
	OperationTags   int // встречено тегов <ApiOperation
	OperationsRead  int // из них разобрано (расхождение = неизвестная форма записи)
	Matched         int // операций контракта, найденных на странице
	UndocumentedRPC int // операций контракта, не найденных нигде
}

func (c APISurfaceCensus) String() string {
	return fmt.Sprintf("перепись: служб %d · методов %d (из них с REST-адресом %d) · "+
		"страниц %d · тегов <ApiOperation %d (разобрано %d) · сошлось %d · не описано %d",
		c.Services, c.RPCs, c.RESTOperations, c.Pages,
		c.OperationTags, c.OperationsRead, c.Matched, c.UndocumentedRPC)
}

// normalizeAddress приводит адрес к виду, одинаковому для контракта и страницы.
//
// Снимаются ровно три различия: шаблон параметра (`{repository=**}` → `{repository}`),
// стиль имени параметра (`{registry_id}` → `{registryId}`) и завершающая косая черта.
// Больше ничего: нормализация, снимающая различия по своему усмотрению, склеила бы
// РАЗНЫЕ адреса в один и объявила бы описанным то, чего на странице нет.
func normalizeAddress(addr string) string {
	addr = pathParamRe.ReplaceAllStringFunc(addr, func(m string) string {
		name := strings.Trim(m, "{}")
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		return "{" + lowerCamel(name) + "}"
	})
	if len(addr) > 1 {
		addr = strings.TrimSuffix(addr, "/")
	}
	return addr
}

// lowerCamel — `registry_id` → `registryId`. Соответствует проекции имён полей,
// которую делает grpc-gateway, и потому не является нашим соглашением.
func lowerCamel(s string) string {
	parts := strings.Split(s, "_")
	out := parts[0]
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}

// ParseContractOperations разбирает объявления служб протофайла.
//
// Разбор построчный и намеренно простой: предмет — соответствие `rpc` ↔ его
// `option (google.api.http)`, а они лежат подряд в пределах одного блока. Комментарии
// отсекаются, потому что имена служб и адреса встречаются и в них — гейт по
// подстроке краснел бы на собственном объяснении.
func ParseContractOperations(proto string, services map[string]struct{}) ([]ContractOperation, int, int) {
	var (
		ops         []ContractOperation
		curService  string
		curRPC      string
		curAddress  string
		curVerb     string
		depth       int
		rpcCount    int
		serviceSeen = map[string]struct{}{}
	)

	flush := func() {
		if curRPC == "" {
			return
		}
		op := ContractOperation{Service: curService, RPC: curRPC, Verb: "gRPC",
			Address: curService + "/" + curRPC}
		if curAddress != "" {
			op.Verb = strings.ToUpper(curVerb)
			op.Address = normalizeAddress(curAddress)
		}
		ops = append(ops, op)
		curRPC, curAddress, curVerb = "", "", ""
	}

	for _, raw := range strings.Split(proto, "\n") {
		line := stripProtoComment(raw)
		if strings.HasPrefix(strings.TrimSpace(line), "service ") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) >= 2 {
				curService = strings.TrimSuffix(fields[1], "{")
				serviceSeen[curService] = struct{}{}
			}
			depth = 0
		}
		if _, want := services[curService]; !want {
			continue
		}
		if m := rpcRe.FindStringSubmatch(line); m != nil {
			flush()
			curRPC = m[1]
			rpcCount++
			depth = 0
		}
		if curRPC != "" && curAddress == "" {
			if m := httpRuleRe.FindStringSubmatch(line); m != nil {
				curVerb, curAddress = m[1], m[2]
			}
		}
		// Закрывающая скобка блока службы завершает набор: без этого последний
		// метод последней службы терялся бы.
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if curRPC != "" && strings.TrimSpace(line) == "}" && depth <= 0 {
			flush()
		}
	}
	flush()

	rest := 0
	for _, o := range ops {
		if o.Verb != "gRPC" {
			rest++
		}
	}
	return ops, rpcCount, rest
}

// stripProtoComment срезает строчный комментарий, не трогая содержимое строкового
// литерала: адрес живёт в кавычках, и наивное срезание по `//` разрубило бы его.
func stripProtoComment(line string) string {
	inStr := false
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '"' && (i == 0 || line[i-1] != '\\'):
			inStr = !inStr
		case !inStr && line[i] == '/' && i+1 < len(line) && line[i+1] == '/':
			return line[:i]
		}
	}
	return line
}

// ParseDocumentedOperations разбирает объявления операций клиентской страницы и
// возвращает их ключи вместе с числом ВСТРЕЧЕННЫХ тегов — второе число нужно, чтобы
// заметить форму записи, которой анализатор не знает.
func ParseDocumentedOperations(page string) (keys []string, tags int) {
	for _, tag := range apiOperationTagRe.FindAllString(page, -1) {
		tags++
		var method, endpoint string
		for _, a := range apiOperationAttrRe.FindAllStringSubmatch(tag, -1) {
			value := a[3] + a[4] // непустой ровно один из двух видов кавычек
			switch a[1] {
			case "method":
				method = value
			case "endpoint":
				endpoint = value
			}
		}
		if method == "" || endpoint == "" {
			// Тег посчитан, но не разобран: расхождение двух чисел — единственный
			// след такого объявления, и гейт краснеет именно на нём.
			continue
		}
		verb := strings.ToUpper(method)
		if strings.EqualFold(method, "gRPC") {
			verb = "gRPC"
		}
		keys = append(keys, verb+" "+normalizeAddress(endpoint))
	}
	return keys, tags
}

// UndocumentedOperations — операции контракта, которых нет ни на одной странице.
//
// `pages` — содержимое клиентских страниц (ключ роли не играет: страница, на которой
// операция описана, выбирает автор, и требовать конкретную значило бы судить раскладку
// сайта вместо умолчания).
func UndocumentedOperations(ops []ContractOperation, pages map[string]string) ([]ContractOperation, APISurfaceCensus) {
	documented := map[string]struct{}{}
	census := APISurfaceCensus{Pages: len(pages)}
	for _, body := range pages {
		keys, tags := ParseDocumentedOperations(body)
		census.OperationTags += tags
		census.OperationsRead += len(keys)
		for _, k := range keys {
			documented[k] = struct{}{}
		}
	}

	var missing []ContractOperation
	for _, op := range ops {
		if _, ok := documented[op.Key()]; ok {
			census.Matched++
			continue
		}
		missing = append(missing, op)
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].RPC < missing[j].RPC })
	census.UndocumentedRPC = len(missing)
	return missing, census
}
