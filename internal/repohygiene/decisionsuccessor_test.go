// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// decisionsuccessor_test.go — ГЕЙТ: все поверхности решения об удалении проекта
// называют ОДНУ задачу-преемника, и она объявлена в одном месте.
//
// Разбор и его границы — `decisionsuccessor.go`.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/contractroot"
	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// decisionProtoPrefix — приставка, по которой координата документа опознаётся как
// координата ДЕРЕВА КОНТРАКТОВ. Одна форма, а не перечень: документ пишет
// координату от корня репозитория.
const decisionProtoPrefix = "proto/"

// decisionSurfacePath — путь на диске к поверхности, названной документом решения,
// и признак того, что она приехала МОДУЛЕМ, а не лежит в этом дереве.
//
// Координата дерева контрактов резолвится contractsource, а не склейкой с корнем
// репозитория. Решением владельца (kacho#2616, исход C, 2026-09-13) контракты
// службы доступа уехали в её собственный репозиторий: под `proto/` их здесь нет,
// а платформе они по-прежнему нужны, и приезжают они опубликованным модулем
// `github.com/PRO-Robotech/kaname`. Склейка с корнем дала бы путь, которого нет, —
// то есть гейт объявил бы «документ называет координату, которой в дереве нет»
// там, где координата верна, а неверен был предикат. Исходы это РАЗНЫЕ: первый
// чинится правкой документа, второй — переводом координаты.
func decisionSurfacePath(root, rel string) (path string, external bool, err error) {
	slash := filepath.ToSlash(rel)
	relToProto := ""
	switch {
	case strings.HasPrefix(slash, decisionProtoPrefix):
		relToProto = strings.TrimPrefix(slash, decisionProtoPrefix)
	default:
		// Координата дерева контрактов бывает названа и БЕЗ приставки `proto/` —
		// именно так её несут дескрипторы, ведомости входов и сообщения buf
		// (`kaname/cloud/iam/v1/project_service.proto`). После переезда контрактов
		// службы (kacho#2616, исход C) это стало ЕДИНСТВЕННОЙ верной формой для её
		// файлов: каталога `proto/kaname` в этом дереве нет, и приставка называла
		// бы место, которого не существует.
		//
		// Форма распознаётся по ПЕРВОМУ СЕГМЕНТУ, сверенному с объявленным
		// множеством корней, а не по догадке о «похоже на контракт»: путь
		// `docs/architecture/…` первым сегментом корня не несёт и уходит в
		// ветвь дерева.
		first := strings.SplitN(slash, "/", 2)[0]
		for _, r := range contractroot.Roots {
			if first == r {
				relToProto = slash
				break
			}
		}
		if relToProto == "" {
			return filepath.Join(root, filepath.FromSlash(rel)), false, nil
		}
	}
	inTree := filepath.Join(root, filepath.FromSlash(slash))
	if _, serr := os.Stat(inTree); serr == nil {
		return inTree, false, nil
	}
	p, perr := contractsource.Path(root, relToProto)
	if perr != nil {
		return "", false, perr
	}
	return p, true, nil
}

// projectDeletionDecisionDoc — документ решения. Координата ОДНА и здесь
// выписана намеренно: это и есть тот единственный вход, из которого гейт выводит
// всё остальное. Переедет документ — гейт скажет об этом отказом, а не молчанием.
const projectDeletionDecisionDoc = "docs/architecture/project-deletion-and-live-resources.md"

// TestProjectDeletionSurfacesNameTheSameSuccessor — сам гейт.
func TestProjectDeletionSurfacesNameTheSameSuccessor(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	doc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(projectDeletionDecisionDoc)))
	if err != nil {
		t.Fatalf("документ решения %s не прочитан: %v — сверять поверхности не с чем, "+
			"и молчание гейта было бы сказано ни о чём", projectDeletionDecisionDoc, err)
	}

	successor := DeclaredSuccessor(doc)
	if successor == 0 {
		t.Fatalf("документ решения %s не объявляет задачу-преемника строкой %q.\n\n"+
			"Решение, принятое и не реализованное, обязано называть держателя остатка. "+
			"Без объявления каждая поверхность называет номер сама, и они расходятся "+
			"молча — в сторону, которая успокаивает: задача, при которой решение "+
			"принималось, закрывается, а ссылки продолжают вести к ней.",
			projectDeletionDecisionDoc, SuccessorMarker)
	}

	refusal := DeclaredRefusalLiterals(doc)
	if len(refusal) == 0 {
		t.Fatalf("документ решения %s не объявляет литералы отказа строкой %q.\n\n"+
			"Механизм посажен (PRO-Robotech/kaname#166), и у решения появилось наблюдаемое: "+
			"тон отказа и его машинный признак. Документ, который их не объявляет, нельзя "+
			"сверить с контрактом — а контракт приезжает модулем, и отставший пин без этой "+
			"сверки выглядел бы согласием.", projectDeletionDecisionDoc, RefusalMarker)
	}

	coords := DeclaredCoordinates(doc)
	census := DecisionCensus{Successor: successor, Coordinates: len(coords)}

	var (
		findings []string
		missing  []string
	)
	external := 0
	literalsChecked := 0
	for _, rel := range coords {
		abs, fromModule, perr := decisionSurfacePath(root, rel)
		if perr != nil {
			// Координата, которую не резолвит ни дерево, ни объявленный модуль
			// её корня: документ посылает читателя туда, где ничего нет, и
			// причина названа предикатом, а не угадывается.
			missing = append(missing, rel+" — "+perr.Error())
			continue
		}
		src, rerr := os.ReadFile(abs) // #nosec G304 -- путь резолвлен из координаты документа решения
		if rerr != nil {
			// Координата, названная документом и НЕ существующая, — находка сама
			// по себе: документ посылает читателя туда, где ничего нет.
			missing = append(missing, rel)
			continue
		}
		if fromModule {
			external++
		}
		census.Surfaces++
		cites, found := CitesSuccessor(src, successor)
		census.Citations += len(found)
		if !cites {
			findings = append(findings, SuccessorFinding(
				DecisionSurface{Path: rel, Cites: cites, Found: found}, successor))
		}
		// Вторая ось: поверхность несёт объявленный отказ ДОСЛОВНО. Для контракта,
		// приехавшего модулем, это и есть сверка пина с документом: до подъёма
		// пина контракт в дереве обещает прежнее поведение, и находка называет
		// ровно то, чего он не несёт.
		literalsChecked += len(refusal)
		if gap := MissingRefusalLiterals(src, refusal); len(gap) > 0 {
			findings = append(findings, RefusalFinding(rel, gap))
		}
	}

	t.Logf("перепись: документ решения %s; объявленная задача-преемник #%d; "+
		"координат прочитано %d, поверхностей разобрано %d (из них приехало модулем %d), "+
		"ссылок на задачи встречено %d; литералов отказа объявлено %d, сверено %d; "+
		"находок %d, ненайденных координат %d",
		projectDeletionDecisionDoc, census.Successor, census.Coordinates,
		census.Surfaces, external, census.Citations, len(refusal), literalsChecked,
		len(findings), len(missing))

	// Предпосылка: поверхности вообще есть. Ноль означает, что документ перестал
	// называть координаты, и суждение выполняется тождественно.
	if census.Surfaces == 0 {
		t.Fatalf("документ решения не назвал НИ ОДНОЙ существующей поверхности "+
			"(координат прочитано %d) — гейт судил бы ни о чём", census.Coordinates)
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("документ решения называет координаты, которых в дереве нет:\n%s",
			strings.Join(missing, "\n"))
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("поверхностей решения, расходящихся с документом: %d\n%s\n\n"+
			"Две оси. ПРЕЕМНИК (#%d): читатель поверхности идёт к задаче, которую она "+
			"называет; если та закрыта, он читает «закрыта» как «сделано» — ссылка лжёт в "+
			"сторону, которая успокаивает. Номер объявляется ОДИН раз (%s в документе "+
			"решения), остальные поверхности обязаны его содержать; историческая ссылка "+
			"на задачу, при которой решение принималось, законна и гейтом не запрещена. "+
			"ОТКАЗ (%s): поверхность, приехавшая модулем, несёт объявленный отказ только "+
			"с той ревизии службы, где механизм посажен, — находка здесь означает пин "+
			"go.mod, отставший от документа, а не ошибку документа.",
			len(findings), strings.Join(findings, "\n"), successor, SuccessorMarker, RefusalMarker)
	}
}
