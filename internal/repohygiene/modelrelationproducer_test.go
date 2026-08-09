// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modelrelationproducer_test.go — гейт против указателя принадлежности, который
// модель объявляет, а никто не пишет.
//
// # Предмет
//
// Структурное отношение модели («этот объект принадлежит тому») существует
// ровно постольку, поскольку кто-то записывает по нему кортеж. Объявленное без
// производителя, оно выглядит в модели как действующая связь: по нему пишут
// правила, на него ссылаются комментарии, его переносят при рефакторинге — и
// каждое из этих действий опирается на связь, которой в данных нет. Отличить
// такое от намеренно спящего отношения по коду нельзя: до этой правки разница
// между «остаток закрытого инцидента» и «объявлено заранее, писателя пока нет»
// была выражена ТОЛЬКО тоном комментария рядом.
//
// # Что требует гейт
//
// У каждого структурного отношения на типе, принадлежащем сервису-потребителю,
// обязан быть либо производитель в дереве, либо МАШИННО РАЗЛИЧИМАЯ пометка
// спящего состояния — строка `# kacho:latent` над объявлением, с причиной.
// Пометка сама истекает: поставленная на отношение, у которого производитель
// ЕСТЬ, она — находка, потому что иначе переживёт свой предмет и будет молча
// разрешать следующему автору снять производителя.
//
// # Объём и его граница, названная честно
//
// Гейт читает типы пяти доменов-потребителей. Типы домена iam сюда не входят:
// их структурные указатели пишет сам iam, напрямую, минуя очередь намерений, и
// производителя для них надо было бы искать другим предикатом. Это ОГРАНИЧЕНИЕ
// ОБЪЁМА, а не исключение из требования: перепись печатает, сколько типов и
// отношений осмотрено, чтобы «ноль находок» было отличимо от «ноль
// прочитанного».
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Канонический источник модели прав — `fgaModelPath` (объявлен соседним гейтом
// этого же пакета): конфигмап чарта порождается из него, поэтому гейту достаточно
// этого файла.

// latentMarker — машинно различимая пометка спящего отношения. Строка с этим
// префиксом над объявлением означает: производителя нет и это решение, а не
// пропуск. Причина обязана стоять в той же строке — пометка без причины
// неотличима от глушилки.
const latentMarker = "# kacho:latent"

// subjectUsersets — вершины, которыми в этой модели записан СУБЪЕКТ (кто), а не
// объект-контейнер (где). Отношение, чей userset состоит только из них, — это
// выдача, её материализует реконсайлер из привязок, и производителя в очереди
// намерений у неё нет by construction.
var subjectUsersets = map[string]struct{}{
	"user": {}, "service_account": {}, "group#member": {}, "user:*": {},
}

var (
	fgaTypeRe   = regexp.MustCompile(`^type\s+([a-z_][a-z0-9_]*)\s*$`)
	fgaDefineRe = regexp.MustCompile(`^\s+define\s+([a-z_][a-z0-9_]*)\s*:\s*\[([^\]]*)\]`)
)

// modelRelation — одно структурное отношение модели.
type modelRelation struct {
	Type     string
	Relation string
	Line     int
	Latent   bool
	Reason   string
}

// TestConsumerOwnedStructuralRelationsHaveProducers — у структурного отношения
// на типе сервиса-потребителя есть производитель либо пометка спящего.
func TestConsumerOwnedStructuralRelationsHaveProducers(t *testing.T) {
	root := repoRoot(t)
	rels, typesSeen, definesSeen := collectStructuralRelations(t, root)

	sites, _ := collectProxyIntentSites(t, root)
	produced := map[string]struct{}{} // "<тип>#<отношение>"
	for _, s := range sites {
		for _, ot := range s.ObjectTypes {
			produced[ot+"#"+s.Relation] = struct{}{}
		}
	}

	var orphans, staleMarkers []string
	consumerRels := 0
	for _, r := range rels {
		if !isConsumerOwnedType(r.Type) {
			continue
		}
		consumerRels++
		_, hasProducer := produced[r.Type+"#"+r.Relation]
		switch {
		case hasProducer && r.Latent:
			staleMarkers = append(staleMarkers, fmt.Sprintf(
				"%s:%d — `%s#%s` помечено спящим, но производитель есть",
				fgaModelPath, r.Line, r.Type, r.Relation))
		case !hasProducer && !r.Latent:
			orphans = append(orphans, fmt.Sprintf(
				"%s:%d — `%s#%s` объявлено, но НИ ОДНА точка эмиссии его не пишет",
				fgaModelPath, r.Line, r.Type, r.Relation))
		}
	}

	t.Logf("осмотрено типов модели: %d; объявлений: %d; из них структурных: %d; "+
		"на типах потребителей: %d; точек эмиссии: %d; помечено спящими: %d",
		typesSeen, definesSeen, len(rels), consumerRels, len(sites), countLatent(rels))

	if consumerRels == 0 {
		t.Fatal("у гейта нет входа: ни одного структурного отношения на типах " +
			"потребителей не найдено. Либо разбор модели перестал совпадать с её " +
			"синтаксисом, либо домены потребителей разошлись с именами типов. " +
			"Молчание здесь означало бы «ничего не прочитано», а не «всё в порядке»")
	}

	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("структурное отношение без производителя (%d):\n  %s\n\n"+
			"Такое отношение выглядит в модели действующей связью: на него ссылаются, "+
			"его переносят при рефакторинге, по нему пишут правила — а связи в данных "+
			"нет. Исход один из двух: снять объявление, либо поставить над ним "+
			"`%s — <причина>`, если писателя ещё нет и это решение. Пометка истекает "+
			"сама: как только производитель появится, вторая половина этой пробы "+
			"потребует её снять.", len(orphans), strings.Join(orphans, "\n  "), latentMarker)
	}

	if len(staleMarkers) > 0 {
		sort.Strings(staleMarkers)
		t.Fatalf("пометка спящего пережила свой предмет (%d):\n  %s\n\n"+
			"Производитель появился, а пометка осталась — и теперь она молча разрешает "+
			"следующему автору снять производителя обратно. Снять пометку.",
			len(staleMarkers), strings.Join(staleMarkers, "\n  "))
	}
}

// TestLatentMarkerCarriesAReason — пометка без причины не считается пометкой.
//
// Иначе она вырождается в глушилку: следующий читатель видит «спящее» и не может
// установить, чего ждут и когда это перестанет быть верным.
func TestLatentMarkerCarriesAReason(t *testing.T) {
	root := repoRoot(t)
	rels, _, _ := collectStructuralRelations(t, root)

	var bare []string
	latent := 0
	for _, r := range rels {
		if !r.Latent {
			continue
		}
		latent++
		if len(strings.TrimSpace(r.Reason)) < 20 {
			bare = append(bare, fmt.Sprintf("%s:%d — `%s#%s`", fgaModelPath, r.Line, r.Type, r.Relation))
		}
	}

	t.Logf("пометок спящего: %d; с причиной: %d", latent, latent-len(bare))

	if len(bare) > 0 {
		t.Fatalf("пометка спящего без причины (%d):\n  %s\n\n"+
			"Пометка без причины — глушилка: она снимает вопрос, не отвечая на него. "+
			"Причина обязана стоять в той же строке.", len(bare), strings.Join(bare, "\n  "))
	}
}

// isConsumerOwnedType — тип принадлежит одному из доменов, регистрирующих свои
// ресурсы через очередь намерений.
func isConsumerOwnedType(t string) bool {
	for _, d := range proxyConsumerDomains {
		if strings.HasPrefix(t, d+"_") {
			return true
		}
	}
	return false
}

func countLatent(rels []modelRelation) int {
	n := 0
	for _, r := range rels {
		if r.Latent {
			n++
		}
	}
	return n
}

// collectStructuralRelations читает модель и отбирает отношения-указатели
// принадлежности: те, чей userset не содержит ни одной субъектной вершины.
// Возвращает также число осмотренных типов и объявлений — перепись отдельным
// утверждением.
func collectStructuralRelations(t *testing.T, root string) ([]modelRelation, int, int) {
	t.Helper()
	path := filepath.Join(root, fgaModelPath)
	body, err := os.ReadFile(path) //nolint:gosec // путь константный, внутри дерева
	if err != nil {
		t.Fatalf("читаю %s: %v", path, err)
	}

	var out []modelRelation
	var curType string
	var pendingReason string
	var haveMarker bool
	typesSeen, definesSeen := 0, 0

	for i, line := range strings.Split(string(body), "\n") {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, latentMarker) {
			haveMarker = true
			pendingReason = strings.TrimSpace(strings.TrimPrefix(trimmed, latentMarker))
			pendingReason = strings.TrimLeft(pendingReason, "—- ")
			continue
		}
		if m := fgaTypeRe.FindStringSubmatch(trimmed); m != nil {
			curType = m[1]
			typesSeen++
			haveMarker, pendingReason = false, ""
			continue
		}
		m := fgaDefineRe.FindStringSubmatch(line)
		if m == nil {
			// Пустая строка и прочие комментарии пометку не переносят: она
			// относится к СЛЕДУЮЩЕМУ объявлению, а не к любому далее.
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				haveMarker, pendingReason = false, ""
			}
			continue
		}
		definesSeen++
		rel, userset := m[1], m[2]
		wasLatent, reason := haveMarker, pendingReason
		haveMarker, pendingReason = false, ""

		if hasSubjectUserset(userset) {
			continue
		}
		out = append(out, modelRelation{
			Type: curType, Relation: rel, Line: lineNo, Latent: wasLatent, Reason: reason,
		})
	}
	return out, typesSeen, definesSeen
}

func hasSubjectUserset(userset string) bool {
	for _, part := range strings.Split(userset, ",") {
		p := strings.TrimSpace(part)
		// `user with mfa_fresh` и подобные — субъект с условием.
		if i := strings.Index(p, " with "); i > 0 {
			p = strings.TrimSpace(p[:i])
		}
		if _, ok := subjectUsersets[p]; ok {
			return true
		}
	}
	return false
}
