// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// peerlaneclassifier_test.go — гейт «полоса ответа соседа не читается вручную
// мимо носителя».
//
// # Предмет
//
// Когда сосед отказывает, вызывающий обязан различить ПОЛОСЫ: чужого объекта нет
// у владельца · объект есть, но состояние не позволяет · владелец недоступен ·
// нам отказано в правах · владелец счёл ссылку негодной (`api-conventions.md`
// §By-lane code-split). От полосы зависит код ответа, право на повтор и то, что
// увидит арендатор.
//
// До носителя (`pkg/peer`) это читалось руками: каждый клиент нёс свой
// `switch status.Code(err)`. Двадцать девять таких мест разошлись предсказуемым
// образом — каждое распознавало ТЕ коды, о которых помнил его автор, а
// остальные сваливались в ветку «прочее»:
//
//   - отказ владельца В ПРАВАХ попадал во «всё остальное» и уезжал арендатору
//     как ВРЕМЕННЫЙ отказ. Повтор его не лечит — решение о правах есть функция
//     от (вызывающий, отношение, объект), и ни одного из трёх повтор не меняет;
//   - собственный истёкший срок не был статусом вовсе и потому не читался ни
//     одной веткой;
//   - «прочее» молча выбирало политику повтора за того, кто её не выбирал.
//
// # Почему гейт, а не разовая правка
//
// Расхождение здесь НЕ ВИДНО по HTTP: по таблице `api-conventions.md` §«gRPC-код
// → HTTP» и INVALID_ARGUMENT, и FAILED_PRECONDITION дают 400, а недоступность
// уезжает 503 только на том вводе, который в пробах обычно не строят. Значит ни
// REST-клиент, ни e2e-утверждение о статусе перехода расхождения не заметят —
// удержать его способна только проверка на уровне Go.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: `status.Code` встречается
// в комментариях, объясняющих эту же полосу (в том числе в комментариях, которые
// НАПИСАЛ этот переход), и текстовый поиск принял бы объяснение за чтение — ровно
// тот класс, который гейт ловит.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// carrierReaders — имена функций НОСИТЕЛЯ, которым читать ответ соседа положено.
// Они и есть та единственная дверь, ради которой гейт существует: `peer.Classify`
// принимает решение, `peer.PeerCode`/`peer.PeerMessage` отдают диагностику.
// Вызов через квалификатор пакета — то есть `peer.X`, а не любой `X`.
var carrierReaders = map[string]bool{
	"Classify":    true,
	"PeerCode":    true,
	"PeerMessage": true,
}

// handRolledReaders — то, чем полосу читают В ОБХОД носителя. Обе формы
// раскладывают ответ соседа по кодам, и обе жили в каждом клиенте своей копией.
var handRolledReaders = map[string]bool{
	"Code":      true, // status.Code(err)
	"FromError": true, // status.FromError(err) → st.Code()
	"Convert":   true, // status.Convert(err).Code()
}

// peerClientRoots — где живут клиенты к СВОИМ соседям. Каталог назван потому,
// что раскладка слоёв в этом продукте нормативна (`architecture.md`): адаптер
// к соседу лежит в `internal/clients/`, и другого дома у него нет.
const peerClientDir = "internal/clients/"

// foreignSystemDirs — адаптеры к ВНЕШНИМ системам. Их ответы — иная полоса: там
// нет ни нашего каталога, ни наших токенов, и словарь из пяти полос к ним
// неприменим by construction. Это не послабление, а граница предмета: носитель
// судит ответ НАШЕГО сервиса.
//
// Признак — подстрока имени файла или каталога. Список замыкается проверкой
// TestForeignSystemDirsHaveSubject: запись, которой больше нечего исключать,
// создаёт впечатление границы, которой нет.
var foreignSystemDirs = map[string]string{
	"hydra":        "провайдер токенов — OAuth2, не наш каталог",
	"/zot/":        "реестр образов — distribution-API, не наш каталог",
	"/jwks/":       "публичные ключи верификации — HTTP, а не gRPC-полоса",
	"provider_hop": "транспорт к провайдеру токенов",
}

// laneExemptions — ЗАКОННЫЕ отступления: файл читает коды соседа не ради полосы,
// либо его полоса принадлежит другому носителю.
//
// У каждой записи — причина и ПРЕДИКАТ СНЯТИЯ. Запись без предиката снятия
// бессрочна by construction и переживает свой предмет; такие записи мы уже
// наследовали как слепые зоны, поэтому здесь их нет.
//
// Замыкается проверкой TestExemptionsHaveSubject: файл, переставший читать коды,
// роняет гейт — послабление истекает само, а не по чьей-то памяти.
var laneExemptions = map[string]struct{ why, until string }{
	"services/vpc/internal/clients/iam_register_applier.go": {
		why:   "полоса РЕГИСТРАЦИИ: вопрос не «что ответить арендатору», а «пройдёт ли повтор у дренажа»",
		until: "решение о повторе очереди перестанет принадлежать pkg/outbox/drainer (drainer.Classify)",
	},
	"services/storage/internal/clients/iam_register_applier.go": {
		why:   "полоса регистрации — см. vpc",
		until: "то же",
	},
	"services/compute/internal/clients/iam_register_applier.go": {
		why:   "полоса регистрации — см. vpc",
		until: "то же",
	},
	"services/nlb/internal/clients/iam/register_applier.go": {
		why:   "полоса регистрации — см. vpc",
		until: "то же",
	},
	"services/registry/internal/clients/iam/register_applier.go": {
		why:   "полоса регистрации — см. vpc",
		until: "то же",
	},
	// ЗДЕСЬ СТОЯЛО ПОСЛАБЛЕНИЕ ДЛЯ ДРЕНАЖА СБРОСА КЭША ВЕРДИКТОВ — файла больше
	// нет (задача #1024). Разбор чужого кода ответа был у него потому, что он
	// дозванивался до КРАЯ; направление развёрнуто, и дозвона не осталось ни
	// одного — во всём дереве владельца прав их теперь ноль. Послабление истекло
	// само, ровно как и должно.
	"services/compute/internal/clients/storage_client.go": {
		why: "ПРОКСИРОВАНИЕ ответа владельца: на пути attach/detach storage отвечает арендатору о " +
			"СВОЁМ ресурсе под личностью вызывающего, и compute передаёт этот ответ как есть. " +
			"Набор кодов включает ALREADY_EXISTS (идемпотентный повтор), которому ни одна из пяти полос " +
			"не соответствует — перевод на носитель переписал бы ответ владельца",
		until: "словарь полос получит конфликт записи (ALREADY_EXISTS) — тогда проксирование выразимо носителем",
	},
	"services/nlb/internal/clients/vpc/internal_address_client.go": {
		why: "МУТИРУЮЩЕЕ внутреннее ребро (allocate/set-reference/clear/delete): промах здесь означает " +
			"идемпотентный УСПЕХ, а ALREADY_EXISTS — конфликт CAS у владельца. Ни то, ни другое не " +
			"является полосой резолва ссылки, и словарь из пяти их не выражает",
		until: "то же — носитель получит полосу конфликта записи и полосу идемпотентного повтора",
	},
}

// laneSiteRead — место, где ответ соседа разбирается по коду.
type laneSiteRead struct {
	file string
	line int
	call string
}

// collectPeerCodeReads обходит дерево клиентов к своим соседям и собирает места,
// где код ответа читается напрямую. Возвращает ещё и перепись: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
func collectPeerCodeReads(t *testing.T, root string) (sites []laneSiteRead, filesRead, inScope int) {
	t.Helper()

	dir := filepath.Join(root, "services")
	err := rootedWalk(dir, func(rel string) bool {
		return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
	}, func(abs string, body []byte) error {
		filesRead++
		rel := filepath.ToSlash(relTo(root, abs))
		if !strings.Contains(rel, peerClientDir) {
			return nil
		}
		if foreignSystemOf(rel) != "" {
			return nil
		}
		inScope++

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, abs, body, 0)
		if perr != nil {
			t.Fatalf("%s: разбор не удался: %v — гейт не вправе засчитать непрочитанный "+
				"файл в перепись", abs, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if pkg.Name == "peer" && carrierReaders[sel.Sel.Name] {
				return true // дверь носителя — законная форма
			}
			if pkg.Name != "status" || !handRolledReaders[sel.Sel.Name] {
				return true
			}
			pos := fset.Position(call.Pos())
			sites = append(sites, laneSiteRead{
				file: filepath.ToSlash(relTo(root, pos.Filename)),
				line: pos.Line,
				call: pkg.Name + "." + sel.Sel.Name,
			})
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v", dir, err)
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].line < sites[j].line
	})
	return sites, filesRead, inScope
}

func foreignSystemOf(rel string) string {
	for marker, why := range foreignSystemDirs {
		if strings.Contains(rel, marker) {
			return why
		}
	}
	return ""
}

// ГЕЙТ — в клиенте к своему соседу ответ разбирается ТОЛЬКО носителем.
//
// Гейт судит код, КОТОРОГО ЕЩЁ НЕТ: сегодняшнее дерево он проходит, а следующего,
// кто заведёт свой `switch status.Code(err)` в клиенте, назовёт координатой.
func TestPeerLaneIsNotReadOutsideTheCarrier(t *testing.T) {
	root := repoRoot(t)
	sites, filesRead, inScope := collectPeerCodeReads(t, root)

	var findings, excused int
	for _, s := range sites {
		if ex, ok := laneExemptions[s.file]; ok {
			excused++
			_ = ex
			continue
		}
		findings++
		t.Errorf("%s:%d — ответ соседа разбирается по коду (%s) в обход носителя.\n"+
			"    Полосу обязан выбирать pkg/peer (peer.Classify): тогда отказ в правах не\n"+
			"    попадёт в ветку «прочее» и не уедет арендатору ВРЕМЕННЫМ, а собственный\n"+
			"    истёкший срок не потеряется по дороге, потому что статусом он не является.\n"+
			"    Если это законное отступление — заведите запись в laneExemptions с причиной\n"+
			"    И ПРЕДИКАТОМ СНЯТИЯ; молчаливое отступление здесь неотличимо от пропуска.",
			s.file, s.line, s.call)
	}

	if inScope == 0 {
		t.Fatalf("предмет гейта не найден: ни одного клиента к своему соседу под %q.\n"+
			"    Прочитано файлов: %d. «Ноль находок» здесь означало бы «ноль прочитанного».",
			peerClientDir, filesRead)
	}
	t.Logf("перепись: прод-файлов прочитано %d, клиентов к своим соседям %d, "+
		"мест разбора по коду %d (находок %d, законных отступлений %d)",
		filesRead, inScope, len(sites), findings, excused)
}

// Каждое послабление обязано иметь ПРЕДМЕТ. Запись, которой больше нечего
// исключать, не безобидна: она создаёт впечатление покрытия, которого нет, и её
// унаследует следующая слепая зона.
func TestExemptionsHaveSubject(t *testing.T) {
	root := repoRoot(t)
	sites, _, _ := collectPeerCodeReads(t, root)

	live := map[string]bool{}
	for _, s := range sites {
		live[s.file] = true
	}
	for file, ex := range laneExemptions {
		if !live[file] {
			t.Errorf("послабление для %s больше нечего исключать: файл не читает код ответа\n"+
				"    соседа (либо его нет в дереве). Причина записи: %q. Уберите запись —\n"+
				"    послабление обязано истекать само.", file, ex.why)
		}
		if strings.TrimSpace(ex.until) == "" {
			t.Errorf("послабление для %s не несёт предиката снятия — оно бессрочно by\n"+
				"    construction и переживёт свой предмет", file)
		}
	}
	t.Logf("перепись: послаблений %d, живых мест разбора %d", len(laneExemptions), len(live))
}

// Граница «внешняя система» обязана иметь предмет по каждой записи — иначе она
// исключает не то, что объявляет.
func TestForeignSystemDirsHaveSubject(t *testing.T) {
	root := repoRoot(t)

	seen := map[string]int{}
	dir := filepath.Join(root, "services")
	var filesRead int
	err := rootedWalk(dir, func(rel string) bool {
		return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
	}, func(abs string, _ []byte) error {
		filesRead++
		rel := filepath.ToSlash(relTo(root, abs))
		if !strings.Contains(rel, peerClientDir) {
			return nil
		}
		for marker := range foreignSystemDirs {
			if strings.Contains(rel, marker) {
				seen[marker]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v", dir, err)
	}

	for marker, why := range foreignSystemDirs {
		if seen[marker] == 0 {
			t.Errorf("признак внешней системы %q (%s) не совпал ни с одним файлом клиентов —\n"+
				"    граница предмета объявлена шире, чем есть", marker, why)
		}
	}
	t.Logf("перепись: признаков внешних систем %d, файлов прочитано %d, совпадений %v",
		len(foreignSystemDirs), filesRead, seen)
}
