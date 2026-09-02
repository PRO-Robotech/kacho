// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_producer_reachable_injection_test.go — доказательство того,
// что проверка ИСПОЛНИМОСТИ вызова производителя способна упасть, что она падает
// НЕ НА ВСЁМ и что она молчит на законных близнецах (задача #1901).
//
// Прогонов ТРИ, и третий обязателен: без него молчание уже существующей соседней
// проверки неотличимо от её смерти (testing.md §«Гейт на класс», п. 2в).
//
//	контроль          — целое дерево: молчат ОБЕ проверки;
//	инъекция новой    — дефект нового предмета (страж возвращён цели): краснеет
//	                    ТОЛЬКО новая, и называет ОБА боевых пути;
//	инъекция прежней  — дефект прежнего предмета (вызов производителя снят):
//	                    краснеет ТОЛЬКО прежняя.
//
// Инъекция нового предмета выбрана так, чтобы ронять ТОЛЬКО его: она возвращает
// предпосылку той цели, которая её и несла, — то есть воспроизводит ровно то
// состояние дерева, что было до починки. Форма «завести ещё одну цель» здесь
// негодна: новая цель нарушила бы всё, что вообще требуется от целей, и красное
// пришло бы от соседа.
//
// Сверх трёх прогонов — по одному близнецу на КАЖДУЮ законную форму, которую
// распознаватель обязан знать: путь, пинящий СЕБЯ к kind; путь на настоящем
// кластере, зовущий цель БЕЗ kind-стража; однокоренное имя стража; упоминание
// стража в комментарии; и пустой обход, на котором вердикт беспредметен.

import (
	"strings"
	"testing"
)

// guardedProducerHeader — заголовок цели производителя с ВОЗВРАЩЁННЫМ стражем.
// Он и есть инъекция: состояние дерева до починки #1901.
const guardedProducerHeader = "module-manifests-configmap: guard-kind-context\n"

// plainProducerHeader — заголовок после починки: страж принадлежит вызывающему.
const plainProducerHeader = "module-manifests-configmap:\n"

// TestProducerReachabilityAuditFallsAndStaysSilentOnItsTwin — прогонов три плюс
// близнецы по каждой законной форме.
func TestProducerReachabilityAuditFallsAndStaysSilentOnItsTwin(t *testing.T) {
	carriers := bringUpCarriers(t)

	// ── ПРОГОН 1: КОНТРОЛЬ. Молчат обе.
	findings, census := auditProducerReachable(carriers)
	t.Logf("контроль: %s", census.Summary())
	if len(findings) != 0 {
		t.Fatalf("контроль: на целом дереве находок %d: %v", len(findings), findings)
	}
	if f, _ := auditBringUpPaths(carriers); len(f) != 0 {
		t.Fatalf("контроль: соседняя проверка нашла %d — красное пришло бы от соседа: %v", len(f), f)
	}

	// ── ПРОГОН 2: ИНЪЕКЦИЯ НОВОГО. Страж возвращён цели производителя.
	injected := make([]deployCarrier, 0, len(carriers))
	replaced := 0
	for _, c := range carriers {
		if strings.Contains(c.Text, plainProducerHeader) {
			c.Text = strings.Replace(c.Text, plainProducerHeader, guardedProducerHeader, 1)
			replaced++
		}
		injected = append(injected, c)
	}
	if replaced != 1 {
		t.Fatalf("инъекция не нашла заголовка цели производителя (заменено %d) — "+
			"она подала бы проверке НЕизменённое дерево, и её молчание ничего не значило бы",
			replaced)
	}
	badFindings, badCensus := auditProducerReachable(injected)
	if len(badFindings) == 0 {
		t.Fatalf("инъекция: страж возвращён цели, а проверка молчит (%s) — "+
			"она не измеряет своего предмета", badCensus.Summary())
	}
	if badCensus.Units != census.Units {
		t.Errorf("инъекция изменила число путей выкатки (%d против %d) — она уронила "+
			"не то, что проверяется", badCensus.Units, census.Units)
	}
	joined := strings.Join(badFindings, "\n")
	for _, want := range []string{"stack-up", "cutover-fe3455.sh", "module-manifests-configmap"} {
		if !strings.Contains(joined, want) {
			t.Errorf("находка не называет %q — чинить придётся перебором: %v", want, badFindings)
		}
	}
	if strings.Contains(joined, "dev-up") {
		t.Errorf("находка называет dev-up — путь, пинящий СЕБЯ к kind, обвинён напрасно: %v",
			badFindings)
	}
	if f, _ := auditBringUpPaths(injected); len(f) != 0 {
		t.Errorf("инъекция нового предмета покраснила СОСЕДНЮЮ проверку (%v) — красное "+
			"приходит от соседа, и новая могла бы оказаться вакуумной", f)
	}

	// ── ПРОГОН 3: ИНЪЕКЦИЯ ПРЕЖНЕГО. Вызов производителя снят отовсюду.
	// Краснеет ТОЛЬКО соседняя: значит её молчание в прогоне 2 было молчанием
	// живой проверки, а не мёртвой.
	// Замена — имя, целью НЕ являющееся. `seed-geo` (как у соседа в его файле)
	// здесь негодно: это kind-only цель, и инъекция уронила бы НОВУЮ проверку
	// по существу верно, но не тем предметом, — красное пришло бы от соседа.
	stripped := make([]deployCarrier, 0, len(carriers))
	for _, c := range carriers {
		c.Text = strings.ReplaceAll(c.Text, manifestProducerToken, "no-such-step-at-all")
		stripped = append(stripped, c)
	}
	if f, _ := auditBringUpPaths(stripped); len(f) == 0 {
		t.Fatal("соседняя проверка молчит на снятом вызове производителя — её молчание " +
			"в прогоне 2 ничего не доказывало")
	}
	if f, _ := auditProducerReachable(stripped); len(f) != 0 {
		t.Errorf("инъекция прежнего предмета покраснила НОВУЮ проверку: %v", f)
	}

	// ── БЛИЗНЕЦ 1: путь, пинящий СЕБЯ к kind, зовёт kind-only цель — законно.
	// Это ровно `dev-up`, и находки быть не должно.
	kindPath := []deployCarrier{syntheticCarrier("deploy/Makefile", "Makefile",
		"guard-kind-context:\n"+
			"\t@ctx=\"$$(kubectl config current-context)\"; \\\n"+
			"\tcase \"$$ctx\" in kind-$(CLUSTER_NAME)) ;; *) exit 1 ;; esac\n"+
			"producer: guard-kind-context\n"+
			"\t@echo produce\n"+
			"up:\n"+
			"\t$(MAKE) guard-kind-context; \\\n"+
			"\t$(MAKE) producer; \\\n"+
			"\thelm upgrade --install rel ./helm/umbrella -n kacho\n")}
	if f, c := auditProducerReachable(kindPath); len(f) != 0 {
		t.Errorf("законный близнец покраснел (%v): путь, пинящий СЕБЯ к kind, вправе звать "+
			"kind-only цель; перепись: %s", f, c.Summary())
	}

	// ── БЛИЗНЕЦ 2: путь на настоящем кластере зовёт цель БЕЗ kind-стража —
	// законно и обязано молчать. Это состояние дерева после починки.
	realPath := []deployCarrier{syntheticCarrier("deploy/Makefile", "Makefile",
		"guard-kind-context:\n"+
			"\t@ctx=\"$$(kubectl config current-context)\"; \\\n"+
			"\tcase \"$$ctx\" in kind-$(CLUSTER_NAME)) ;; *) exit 1 ;; esac\n"+
			"producer:\n"+
			"\t@echo produce\n"+
			"stack-up:\n"+
			"\t$(MAKE) producer; \\\n"+
			"\thelm upgrade --install rel ./helm/umbrella -n kacho\n")}
	if f, c := auditProducerReachable(realPath); len(f) != 0 {
		t.Errorf("законный близнец покраснел (%v): цель без kind-стража исполнима на любом "+
			"стенде; перепись: %s", f, c.Summary())
	}

	// ── БЛИЗНЕЦ 3: ОДНОКОРЕННОЕ ИМЯ. Путь зовёт `guard-kind-context-lite` —
	// другую цель, стражем не являющуюся. Сверка ПОДСТРОКОЙ зачла бы её за
	// `guard-kind-context` и в обе стороны солгала бы: путь стал бы «пинящим
	// себя к kind», а цель — «отвергающей чужой контекст».
	lookalike := []deployCarrier{syntheticCarrier("deploy/Makefile", "Makefile",
		"guard-kind-context:\n"+
			"\t@ctx=\"$$(kubectl config current-context)\"; \\\n"+
			"\tcase \"$$ctx\" in kind-$(CLUSTER_NAME)) ;; *) exit 1 ;; esac\n"+
			"guard-kind-context-lite:\n"+
			"\t@echo ничего не проверяю\n"+
			"producer: guard-kind-context\n"+
			"\t@echo produce\n"+
			"stack-up:\n"+
			"\t$(MAKE) guard-kind-context-lite; \\\n"+
			"\t$(MAKE) producer; \\\n"+
			"\thelm upgrade --install rel ./helm/umbrella -n kacho\n")}
	lf, lc := auditProducerReachable(lookalike)
	if len(lf) == 0 {
		t.Errorf("однокоренное имя зачтено за стража: путь зовёт `guard-kind-context-lite`, "+
			"который ничего не пинит, и всё же признан пинящим себя к kind; перепись: %s",
			lc.Summary())
	}

	// ── БЛИЗНЕЦ 4: страж, названный ТОЛЬКО в комментарии, предпосылкой не
	// является — иначе проверка зачла бы за провязку собственное объяснение.
	commented := []deployCarrier{syntheticCarrier("deploy/Makefile", "Makefile",
		"guard-kind-context:\n"+
			"\t@ctx=\"$$(kubectl config current-context)\"; \\\n"+
			"\tcase \"$$ctx\" in kind-$(CLUSTER_NAME)) ;; *) exit 1 ;; esac\n"+
			"# producer: guard-kind-context — так было до починки\n"+
			"producer:\n"+
			"\t@echo produce\n"+
			"stack-up:\n"+
			"\t$(MAKE) producer; \\\n"+
			"\thelm upgrade --install rel ./helm/umbrella -n kacho\n")}
	if f, c := auditProducerReachable(commented); len(f) != 0 {
		t.Errorf("страж из комментария зачтён за предпосылку (%v) — проверка судит текст, "+
			"а не исполняемую часть; перепись: %s", f, c.Summary())
	}
}

// TestProducerReachabilityRefusesTheEmptyWalk — на пустом обходе вердикта нет.
// Без этой пробы «ноль находок» было бы неотличимо от «ноль прочитанного», и
// проверка зеленела бы над деревом, которого не читала.
func TestProducerReachabilityRefusesTheEmptyWalk(t *testing.T) {
	findings, census := auditProducerReachable(nil)
	if len(findings) != 0 {
		t.Fatalf("на пустом обходе найдено %d — находка из ничего: %v", len(findings), findings)
	}
	for name, got := range map[string]int{
		"носителей":      census.Carriers,
		"целей Makefile": census.Targets,
		"kind-стражей":   census.KindGuards,
		"путей выкатки":  census.Units,
		"вызовов":        census.CallsJudged,
	} {
		if got != 0 {
			t.Errorf("на пустом обходе %s = %d — перепись выдумывает предмет", name, got)
		}
	}
	// Ровно на этих нулях главная проба ОТКАЗЫВАЕТ вынести вердикт; проверяем,
	// что отказ имеет предмет, а не остаётся объявлением.
	if census.Summary() == "" {
		t.Fatal("перепись пуста — «ноль находок» стало бы неотличимо от «ноль прочитанного»")
	}
}
