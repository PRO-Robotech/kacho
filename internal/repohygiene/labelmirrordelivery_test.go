// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// labelmirrordelivery_test.go — гейт против ОДНОСТОРОННЕЙ материализации меток.
//
// Доступ, выданный по метке, отзывается СНЯТИЕМ метки. Отзыв доезжает до
// владельца прав только вместе с обновлённым зеркалом ресурса, поэтому «когда
// доедет зеркало» и есть «когда снимется доступ».
//
// Создание ресурса доставляет свою регистрацию ДВАЖДЫ: durable-intent в
// writer-TX (at-least-once дренаж) плюс синхронный вызов сразу после коммита —
// поэтому владелец видит свой свежий ресурс на пути запроса. Смена меток на
// Update долгое время доставлялась ТОЛЬКО первым способом: intent писался,
// синхронной доставки не было, и обновление зеркала уезжало в очередь.
//
// Замер, из которого выведен гейт (стенд 2026-08-05, ревизия 1e94ff69): под
// конкурентной волной строки intent'ов, порождённые Update'ами, лежали в
// `kacho_vpc.fga_register_outbox` от 188 до 365 с (среднее по трём часам — 278 с,
// p95 — 446 с, максимум — 1193 с), тогда как клиентский бюджет чтения-своих-
// записей — 15 с. Семь e2e-утверждений о снятии/добавлении метки падали именно
// на этом, и падали только под нагрузкой: на пустой очереди тот же прогон
// зелёный, потому что дренаж успевал. То есть дефект был НЕ в пробе и не в
// «медленно вообще», а в АСИММЕТРИИ ДВУХ ПОЛОВИН ОДНОГО КОНТРАКТА: выдача по
// метке применялась на пути запроса, отзыв — когда до него дойдёт очередь.
//
// Что утверждает гейт: функция, которая РАЗВЕТВЛЯЕТСЯ на «метки затронуты»,
// обязана содержать синхронную доставку. Утверждается ИСХОД пути (доставили),
// а не факт эмиссии (положили в очередь) — аддитивный путь зеленеет на всех
// утверждениях «эмитировали» и неверен ровно в том, ради чего отзыв делается.
//
// Предпосылка гейта проверяется им самим: обе переписи именованы, объём
// осмотренного печатается, и КАЖДЫЙ сервис из объявленного набора обязан дать
// хотя бы одну площадку — иначе переименование дискриминатора тихо опустошило бы
// перепись и «ноль находок» стало бы неотличимо от «ноль прочитанного».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// labelTouchedDiscriminators — имена, которыми в дереве выражается решение
// «Update затронул метки». Перепись, а не догадка: каждое имя названо вместе с
// сервисом, который его ввёл. Новое имя добавляется сюда вместе со своей
// площадкой, иначе площадка гейту невидима.
var labelTouchedDiscriminators = map[string]string{
	"labelsInMask":         "vpc (7 ресурсов) + compute (instance)",
	"listenerLabelsInMask": "nlb listener",
	"labelsInMaskTG":       "nlb targetGroup",
	"emitMirror":           "nlb (loadBalancer/targetGroup/listener)",
	"LabelsSet":            "storage (volume/image/snapshot)",
}

// syncLabelDelivery — имена синхронной доставки зеркала после коммита. Тоже
// перепись: гейт не угадывает «что-то похожее на регистрацию», он знает четыре
// точки, через которые доставка в этом дереве проходит.
var syncLabelDelivery = map[string]string{
	"DeliverAfterCommit": "vpc — fgaregister.DeliverAfterCommit",
	"syncRegister":       "nlb — UseCase.syncRegister",
	// compute: имя переехало в общий пакет `shared/ownersync`, когда синхронную
	// доставку стал звать не только ресурс машины. Гейт знает точки доставки
	// ПОИМЁННО, поэтому переименование делает площадку ему невидимой — что он и
	// показал в тот же прогон. Здесь стоит короткое имя вызова (`ownersync.Register`
	// разбирается как селектор `Register`).
	"Register":           "compute — ownersync.Register",
	"registerOwnerTuple": "storage — UseCase.registerOwnerTuple",
}

// labelMirrorServices — сервисы, обязанные быть представлены в переписи. Пустой
// вклад любого из них — находка: значит либо площадку переименовали, либо её
// удалили, и в обоих случаях вердикт перестал быть про то дерево, что есть.
var labelMirrorServices = []string{"vpc", "nlb", "compute", "storage"}

// TestLabelMirrorIsDeliveredNotOnlyQueued — смена меток доставляется синхронно
// везде, где она вообще различается.
func TestLabelMirrorIsDeliveredNotOnlyQueued(t *testing.T) {
	root := repoRoot(t)
	sites, filesRead := scanLabelMirrorSites(t, root)

	if filesRead == 0 {
		t.Fatalf("предпосылка гейта нарушена: прочитано 0 файлов — вердикт был бы о пустом дереве")
	}
	if len(sites) == 0 {
		t.Fatalf("предпосылка гейта нарушена: ни одной площадки «метки затронуты» на %d прочитанных файлах; "+
			"словарь дискриминаторов (%d имён) разошёлся с деревом", filesRead, len(labelTouchedDiscriminators))
	}

	byService := map[string]int{}
	var findings []string
	for _, s := range sites {
		byService[s.service]++
		if !s.delivers {
			findings = append(findings, s.where+": функция ветвится на «метки затронуты» ("+s.discriminator+
				"), но синхронной доставки зеркала в ней нет — обновление уезжает только в очередь, "+
				"и отзыв по снятию метки ждёт её глубины")
		}
	}
	for _, svc := range labelMirrorServices {
		if byService[svc] == 0 {
			findings = append(findings, "перепись пуста для сервиса "+svc+
				": площадка переименована или удалена — гейт перестал бы её видеть молча")
		}
	}

	t.Logf("осмотрено: %d файлов, площадок «метки затронуты»: %d (%s)",
		filesRead, len(sites), formatCounts(byService))

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("доставка зеркала меток односторонняя в %d месте(ах):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// labelMirrorSite — одна найденная площадка.
type labelMirrorSite struct {
	service       string
	where         string
	discriminator string
	delivers      bool
}

// scanLabelMirrorSites разбирает use-case-слой каждого сервиса и возвращает
// площадки вместе с числом ПРОЧИТАННЫХ файлов (перепись — отдельное
// утверждение от вердикта).
func scanLabelMirrorSites(t *testing.T, root string) ([]labelMirrorSite, int) {
	t.Helper()
	var sites []labelMirrorSite
	filesRead := 0

	for _, svc := range labelMirrorServices {
		dir := filepath.Join(root, "services", svc, "internal", "apps")
		err := rootedWalk(dir,
			func(rel string) bool {
				return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
			},
			func(abs string, body []byte) error {
				filesRead++
				fset := token.NewFileSet()
				f, perr := parser.ParseFile(fset, abs, body, 0)
				if perr != nil {
					t.Fatalf("предпосылка гейта нарушена: %s не разбирается: %v", abs, perr)
				}
				rel, _ := filepath.Rel(root, abs)
				ast.Inspect(f, func(n ast.Node) bool {
					var fnBody *ast.BlockStmt
					var fnName string
					switch d := n.(type) {
					case *ast.FuncDecl:
						fnBody, fnName = d.Body, d.Name.Name
					case *ast.FuncLit:
						fnBody, fnName = d.Body, "func-literal"
					default:
						return true
					}
					if fnBody == nil {
						return true
					}
					disc, ok := labelDiscriminatorOf(fnBody)
					if !ok {
						return true
					}
					sites = append(sites, labelMirrorSite{
						service:       svc,
						where:         rel + ":" + fnName,
						discriminator: disc,
						delivers:      hasSyncDelivery(fnBody),
					})
					return true
				})
				return nil
			})
		if err != nil {
			t.Fatalf("обход %s: %v", dir, err)
		}
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].where < sites[j].where })
	return sites, filesRead
}

// labelDiscriminatorOf сообщает, ветвится ли тело на «метки затронуты», и как
// это решение названо. Читается ТОЛЬКО условие `if` — присваивание флага
// (`labelsInMask = true`) решением не является, иначе гейт считал бы площадкой
// и место, где флаг лишь вычисляется.
//
// Во вложенные функциональные литералы обход НЕ спускается: рабочее тело
// операции — самостоятельная площадка со своим вердиктом, и засчитывать её
// дважды (телу-обёртке и самому литералу) значило бы напечатать перепись вдвое
// больше дерева.
func labelDiscriminatorOf(body *ast.BlockStmt) (string, bool) {
	var found string
	ast.Inspect(body, func(n ast.Node) bool {
		if _, nested := n.(*ast.FuncLit); nested {
			return false // ВЛОЖЕННОЕ тело — своя площадка, здесь не засчитывается
		}
		ifs, ok := n.(*ast.IfStmt)
		if !ok || found != "" {
			return found == ""
		}
		ast.Inspect(ifs.Cond, func(c ast.Node) bool {
			switch e := c.(type) {
			case *ast.Ident:
				if _, ok := labelTouchedDiscriminators[e.Name]; ok {
					found = e.Name
				}
			case *ast.SelectorExpr:
				if _, ok := labelTouchedDiscriminators[e.Sel.Name]; ok {
					found = e.Sel.Name
				}
			}
			return found == ""
		})
		return found == ""
	})
	return found, found != ""
}

// hasSyncDelivery сообщает, содержит ли тело вызов синхронной доставки зеркала.
// Разбирается AST, а не текст: упоминание имени в КОММЕНТАРИИ, объясняющем эту
// же доставку, вызовом не является (иначе гейт зеленел бы на снятой доставке —
// ровно тот класс, который он ловит).
func hasSyncDelivery(body *ast.BlockStmt) bool {
	delivers := false
	ast.Inspect(body, func(n ast.Node) bool {
		if _, nested := n.(*ast.FuncLit); nested {
			return false // симметрично: доставка вложенного тела принадлежит ЕМУ
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || delivers {
			return !delivers
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			_, delivers = syncLabelDelivery[fn.Name]
		case *ast.SelectorExpr:
			_, delivers = syncLabelDelivery[fn.Sel.Name]
		}
		return !delivers
	})
	return delivers
}

func formatCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}
