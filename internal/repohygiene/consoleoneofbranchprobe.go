// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Разбор контракта и реестров консоли для гейта «ветвь контракта, достижимая из
// создания, обязана быть выразима формой ТОГО модуля, который её рисует».
//
// Вынесено в не-тестовый файл пакета, чтобы инъекционная проба звала тот же
// разбор, а не свою копию: копия разошлась бы с оригиналом молча и доказывала бы
// способность упасть у кода, который не исполняется.

// consoleSpec — запись реестра консоли: какой модуль её объявляет и создаёт ли.
type consoleSpec struct {
	Module    string // `shared`, `nlb`, `compute`, …
	ID        string // `target-groups`
	APIPath   string // `/nlb/v1/targetGroups`
	Creatable bool   // объявлен ли `ops.create: true`
	File      string // путь реестра, для координаты в отказе
}

// protoField — поле сообщения контракта; Oneof непуст, если поле внутри группы.
type protoField struct {
	Type  string
	Name  string
	Oneof string
}

// protoCreate — мутирующий RPC, у которого край объявляет маршрут.
type protoCreate struct {
	RPC     string
	Request string
	Path    string
}

// protoFile — разобранный файл контракта.
type protoFile struct {
	Messages map[string][]protoField
	Creates  []protoCreate
}

var (
	reProtoPackage = regexp.MustCompile(`(?m)^\s*package\s+([\w.]+)\s*;`)
	reProtoMessage = regexp.MustCompile(`^\s*message\s+(\w+)\s*\{`)
	reProtoOneof   = regexp.MustCompile(`^\s*oneof\s+(\w+)\s*\{`)
	// Поле контракта: тип, имя, номер и НЕОБЯЗАТЕЛЬНЫЙ хвост опций. Без хвоста
	// разбор молча терял ветви вида `string subnet_id = 2 [(length) = "<=50"];`
	// — то есть возвращал бы «ветвей нет» и зеленил бы сверку целиком.
	reProtoField = regexp.MustCompile(`^\s*(?:repeated\s+|optional\s+)?([\w.]+)\s+([a-z_][\w]*)\s*=\s*\d+\s*(?:\[[^\]]*\])?\s*;`)
	reProtoRPC   = regexp.MustCompile(`^\s*rpc\s+(\w+)\s*\(\s*([\w.]+)\s*\)`)
	reHTTPRule   = regexp.MustCompile(`(?:post|put|patch)\s*:\s*"([^"]+)"`)

	reSpecHead    = regexp.MustCompile(`(?m)^\s{2}"?[a-z0-9-]+"?:\s*\{\s*\n\s*id:\s*"([a-z0-9-]+)"`)
	reSpecAPIPath = regexp.MustCompile(`apiPath:\s*"([^"]+)"`)
	reSpecOps     = regexp.MustCompile(`ops:\s*\{([^}]*)\}`)
	reOpsCreate   = regexp.MustCompile(`create:\s*true`)
)

type protoFrame struct {
	name  string
	depth int
}

func qualify(pkg string, msgs []protoFrame) string {
	names := make([]string, 0, len(msgs))
	for _, m := range msgs {
		names = append(names, m.name)
	}
	return pkg + "." + strings.Join(names, ".")
}

// parseProtoFile разбирает один файл контракта.
func parseProtoFile(body string) protoFile {
	out := protoFile{Messages: map[string][]protoField{}}
	pkg := ""
	if m := reProtoPackage.FindStringSubmatch(body); m != nil {
		pkg = m[1]
	}
	lines := strings.Split(body, "\n")

	var msgs, oneofs []protoFrame
	depth := 0

	for i, line := range lines {
		switch {
		case reProtoMessage.MatchString(line):
			msgs = append(msgs, protoFrame{name: reProtoMessage.FindStringSubmatch(line)[1], depth: depth})
			key := qualify(pkg, msgs)
			if _, ok := out.Messages[key]; !ok {
				out.Messages[key] = nil
			}
		case reProtoOneof.MatchString(line):
			oneofs = append(oneofs, protoFrame{name: reProtoOneof.FindStringSubmatch(line)[1], depth: depth})
		case len(msgs) > 0 && !strings.HasPrefix(strings.TrimSpace(line), "//"):
			if m := reProtoField.FindStringSubmatch(line); m != nil {
				o := ""
				if len(oneofs) > 0 {
					o = oneofs[len(oneofs)-1].name
				}
				key := qualify(pkg, msgs)
				out.Messages[key] = append(out.Messages[key], protoField{Type: m[1], Name: m[2], Oneof: o})
			}
		}

		if m := reProtoRPC.FindStringSubmatch(line); m != nil {
			if p := httpPathOf(lines, i); p != "" {
				out.Creates = append(out.Creates, protoCreate{RPC: m[1], Request: pkg + "." + m[2], Path: p})
			}
		}

		depth += strings.Count(line, "{") - strings.Count(line, "}")
		for len(oneofs) > 0 && depth <= oneofs[len(oneofs)-1].depth {
			oneofs = oneofs[:len(oneofs)-1]
		}
		for len(msgs) > 0 && depth <= msgs[len(msgs)-1].depth {
			msgs = msgs[:len(msgs)-1]
		}
	}
	return out
}

// httpPathOf ищет объявление маршрута в пределах блока RPC.
func httpPathOf(lines []string, from int) string {
	for j := from; j < len(lines) && j < from+40; j++ {
		if m := reHTTPRule.FindStringSubmatch(lines[j]); m != nil {
			return m[1]
		}
		if j > from && strings.TrimSpace(lines[j]) == "}" {
			return ""
		}
	}
	return ""
}

// branchingsReachable перечисляет группы `oneof`, достижимые из тела запроса
// root, включая вложенные сообщения того же дерева контрактов.
//
// Возвращает пары «полное имя сообщения :: имя группы» — единицу счёта гейта.
func branchingsReachable(messages map[string][]protoField, root string) []string {
	seen := map[string]bool{}
	found := map[string]bool{}
	var walk func(msg string, depth int)
	walk = func(msg string, depth int) {
		if depth > 6 || seen[msg] {
			return
		}
		seen[msg] = true
		pkg := msg[:strings.LastIndex(msg, ".")]
		for _, f := range messages[msg] {
			if f.Oneof != "" {
				found[msg+"::"+f.Oneof] = true
			}
			if sub := resolveMessage(messages, pkg, f.Type); sub != "" {
				walk(sub, depth+1)
			}
		}
	}
	walk(root, 0)
	out := make([]string, 0, len(found))
	for k := range found {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func resolveMessage(messages map[string][]protoField, pkg, typeName string) string {
	if !strings.HasPrefix(pkg, "kacho.") {
		return ""
	}
	if _, ok := messages[typeName]; ok {
		return typeName
	}
	if cand := pkg + "." + typeName; func() bool { _, ok := messages[cand]; return ok }() {
		return cand
	}
	for k := range messages {
		if strings.HasSuffix(k, "."+typeName) && strings.HasPrefix(k, "kacho.") {
			return k
		}
	}
	return ""
}

// parseConsoleRegistry разбирает реестр ресурсов одного модуля консоли.
func parseConsoleRegistry(module, file, body string) []consoleSpec {
	var out []consoleSpec
	for _, loc := range reSpecHead.FindAllStringSubmatchIndex(body, -1) {
		id := body[loc[2]:loc[3]]
		end := loc[1] + 2500
		if end > len(body) {
			end = len(body)
		}
		seg := body[loc[1]:end]
		ap := reSpecAPIPath.FindStringSubmatch(seg)
		if ap == nil {
			continue
		}
		creatable := false
		if ops := reSpecOps.FindStringSubmatch(seg); ops != nil {
			creatable = reOpsCreate.MatchString(ops[1])
		}
		out = append(out, consoleSpec{Module: module, ID: id, APIPath: ap[1], Creatable: creatable, File: file})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// moduleOfRegistry возвращает имя модуля по пути его реестра относительно `ui-future`.
func moduleOfRegistry(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
