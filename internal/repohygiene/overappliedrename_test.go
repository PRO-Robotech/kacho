// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// overappliedrename_test.go — переименование не применено там, где каталог
// так НЕ назван: координата, у которой резолвится якорь, а переименованный
// сегмент — нет.
//
// # Предмет
//
// Служба доступа получила собственное имя продукта и переименовала свои
// каталоги (`internal/apps/kacho` → `internal/apps/kaname` и далее). Массовая
// правка шире своего предмета задевает соседей, у которых своего имени продукта
// нет и чьи каталоги законно зовутся именем платформы. Получается координата,
// которая не была верна НИКОГДА: ни до правки, ни после.
//
// Это ЗЕРКАЛО класса «утверждение пережило свой предмет». Там координата была
// верна и перестала; здесь — не была верна ни в один момент времени, и потому
// её нельзя найти ни поиском по истории, ни сверкой с прежней ревизией.
// Молчит она тем же способом: комментарий не собирается, не импортируется и
// конфликта при слиянии не даёт.
//
// # Предикат: ЯКОРЬ резолвится, переименованный сегмент — нет
//
// Судится путеподобный токен, среди сегментов которого есть имя продукта
// службы. Якорь — часть токена ДО этого сегмента. Находкой считается токен, у
// которого якорь резолвится в дереве, а якорь ВМЕСТЕ с этим сегментом — нет.
//
// Тогда утверждение звучит буквально «вот существующий каталог, а в нём
// подкаталог с именем продукта», и второй половины в дереве не существует.
//
// Резолв — точным путём либо СУФФИКСОМ отслеживаемого пути: проза пишет
// координату от корня службы (`nlb/internal/repo/...`), а не репозитория. Это
// делает предикат мягче, и направление выбрано осознанно: гейт стережёт
// ВОЗВРАЩЕНИЕ несуществующей координаты, а ложная находка на живой отключила бы
// его первой же.
//
// # Почему якорь обязан быть не короче двух сегментов
//
// Имя продукта — ещё и обычное слово прозы, и проза перечисляет службы через
// косую черту: «единый пол безопасности, идентичный kaname/geo/nlb/registry».
// Такой токен путём не является, но односегментный якорь у него резолвится
// (каталоги с этим именем в дереве есть), и предикат с порогом в один сегмент
// объявил бы находкой шесть законных перечислений — измерено на дереве, а не
// предположено.
//
// Порог обрезает и полосу настоящих координат из одного сегмента; она названа
// числом (`shortAnchor`), а не умолчана.
//
// # Полосы названы числами, а не оговорками
//
//	moduleForm       путь модуля Go (`github.com/...`) — резолвится модулем, а
//	                 не деревом; судить его этим предикатом значило бы мерить
//	                 не тот авторитет. СЛЕПАЯ ЗОНА, и её рост виден числом
//	shortAnchor      якорь короче двух сегментов — см. выше
//	anchorUnresolved якорь не резолвится вовсе: токен путём этого дерева не
//	                 является (адрес в контейнере, ресурс кластера, адрес URL,
//	                 синтетическая фикстура чужой пробы)
//
// # Перепись
//
// Печатается: файлов прочитано, двоичных пропущено, токенов с сегментом имени
// продукта, каждая полоса своим числом, судимых, из них резолвится, находок.
// Ноль токенов — ОТКАЗ: гейт, чей предмет отсутствие, молчит одинаково и когда
// предмета нет, и когда сломан обход. Условие самоистекающее.
package repohygiene

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// standaloneProductSegment — имя продукта, которым служба доступа назвала свои
// каталоги. Объявлено ОДИН раз: и обход, и перепись, и инъекция читают отсюда.
const standaloneProductSegment = "kaname"

// minAnchorSegments — нижняя граница длины якоря. Обоснование — в шапке; число
// стоит здесь, а не в регулярке, чтобы его изменение было решением.
const minAnchorSegments = 2

// coordinateTokenRe — путеподобный токен: два и более сегмента через косую
// черту. Знаки подстановки и фигурные скобки включены намеренно — координата в
// прозе пишется с ними (`pg/*.go`, `api/{create`), и токен обязан ловиться
// целиком, иначе якорь считался бы от обрубка.
var coordinateTokenRe = regexp.MustCompile(`[A-Za-z0-9_.*{}$@-]+(?:/[A-Za-z0-9_.*{}$@-]+)+`)

type overAppliedFinding struct {
	file   string
	line   int
	token  string
	anchor string
	text   string
}

func (f overAppliedFinding) String() string {
	return fmt.Sprintf("%s:%d: %s — якорь %s резолвится, %s/%s нет | %s",
		f.file, f.line, f.token, f.anchor, f.anchor, standaloneProductSegment,
		strings.TrimSpace(f.text))
}

type overAppliedCensus struct {
	filesRead        int
	filesBinary      int
	tokensWithName   int
	moduleForm       int
	shortAnchor      int
	anchorUnresolved int
	judged           int
	resolved         int
}

func (c overAppliedCensus) String() string {
	return fmt.Sprintf("файлов прочитано %d · двоичных пропущено %d · токенов с сегментом %q %d "+
		"(путь модуля %d · якорь короче %d сегментов %d · якорь не резолвится %d) · "+
		"судимых %d (резолвится %d)",
		c.filesRead, c.filesBinary, standaloneProductSegment, c.tokensWithName,
		c.moduleForm, minAnchorSegments, c.shortAnchor, c.anchorUnresolved,
		c.judged, c.resolved)
}

// treeCoordinateResolves — путь есть в дереве сам по себе либо суффиксом
// какого-то отслеживаемого пути.
func treeCoordinateResolves(tree *treecorpus.Tree, coord string) bool {
	coord = strings.Trim(coord, "./,;:")
	if coord == "" {
		return false
	}
	if tree.HasFile(coord) || tree.HasDir(coord) {
		return true
	}
	tail := "/" + coord
	for rel := range tree.Files() {
		if strings.HasSuffix(rel, tail) || strings.Contains(rel, tail+"/") {
			return true
		}
	}
	return false
}

// scanOverAppliedRename разбирает ПРОИЗВОЛЬНОЕ дерево: настоящий репозиторий и
// синтетический корень инъекции проходят одну и ту же функцию, поэтому
// доказанное на втором верно для первого.
func scanOverAppliedRename(tree *treecorpus.Tree) (overAppliedCensus, []overAppliedFinding, error) {
	var census overAppliedCensus
	var findings []overAppliedFinding

	root := tree.Root()
	for _, rel := range tree.SortedFiles() {
		slash := filepath.ToSlash(rel)
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			if os.IsNotExist(err) {
				// Индекс называет файл, которого нет на диске (частичная
				// выгрузка). Это не находка о прозе — молчим и не считаем.
				continue
			}
			return census, nil, fmt.Errorf("файл %s не прочитан: %w", slash, err)
		}
		head := raw
		if len(head) > 8192 {
			head = head[:8192]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			census.filesBinary++
			continue
		}
		census.filesRead++
		if !bytes.Contains(raw, []byte(standaloneProductSegment)) {
			continue
		}

		for i, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, standaloneProductSegment) {
				continue
			}
			for _, tok := range coordinateTokenRe.FindAllString(line, -1) {
				tok = strings.TrimRight(tok, "/.,;:)")
				segs := strings.Split(tok, "/")
				k := -1
				for j, s := range segs {
					if s == standaloneProductSegment {
						k = j
						break
					}
				}
				if k < 0 {
					continue
				}
				census.tokensWithName++
				if strings.HasPrefix(tok, "github.com/") || strings.HasPrefix(tok, "PRO-Robotech/") {
					census.moduleForm++
					continue
				}
				if k < minAnchorSegments {
					census.shortAnchor++
					continue
				}
				anchor := strings.Join(segs[:k], "/")
				if !treeCoordinateResolves(tree, anchor) {
					census.anchorUnresolved++
					continue
				}
				census.judged++
				if treeCoordinateResolves(tree, strings.Join(segs[:k+1], "/")) {
					census.resolved++
					continue
				}
				findings = append(findings, overAppliedFinding{slash, i + 1, tok, anchor, line})
			}
		}
	}

	if census.filesRead == 0 {
		return census, nil, fmt.Errorf("обход пуст — вердикт беспредметен (корень %q)", root)
	}
	return census, findings, nil
}

// TestOverAppliedRenameLeavesNoUnresolvableCoordinate — гейт класса.
func TestOverAppliedRenameLeavesNoUnresolvableCoordinate(t *testing.T) {
	tree, err := treecorpus.NewTree(repoRoot(t))
	if err != nil {
		t.Fatalf("состав дерева не собран — вердикт беспредметен: %v", err)
	}

	census, findings, err := scanOverAppliedRename(tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	t.Logf("перепись: %s", census)

	if census.tokensWithName == 0 {
		t.Fatalf("предпосылка не выполняется: в %d прочитанных файлах не найдено ни одного "+
			"путеподобного токена с сегментом %q. Либо имя продукта ушло из координат вовсе "+
			"(тогда гейт снимается вместе с предметом), либо сломан обход — и тогда дерево "+
			"молча не читается, ровно тот дефект, ради которого гейт заведён.\nПерепись: %s",
			census.filesRead, standaloneProductSegment, census)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("%d координат называют подкаталог %q там, где каталог так не назван:%s"+
			"\n\nПереименование каталогов принадлежит службе доступа и только ей: остальные "+
			"службы своего имени продукта не имеют, их каталоги зовутся именем платформы. "+
			"Такая координата не была верна НИ В ОДИН момент времени — правьте на ту, что "+
			"резолвится, а не удаляйте: числа рядом сняты на конкретном пакете и обязаны "+
			"остаться привязанными к нему.\nПерепись: %s",
			len(findings), standaloneProductSegment, b.String(), census)
	}
}
