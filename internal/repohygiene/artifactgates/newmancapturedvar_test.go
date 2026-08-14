// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmancapturedvar_test.go — гейт по дереву: шаг, ЗАХВАТЫВАЮЩИЙ переменную из
// СВОЕГО ответа, обязан нести утверждение — иначе его отказ называет виновником
// невиновного.
//
// # Предмет
//
// Захват записан так, что отказ его не роняет:
//
//	try {
//	  const j = pm.response.json();
//	  const v = (j.id);
//	  if (v !== undefined && v !== null) pm.environment.set('tgId', String(v));
//	} catch (e) {}
//
// Мутация отвергнута — тела с `id` нет, `pm.environment.set` не исполняется,
// `catch` глотает разбор. Шаг зеленеет при ЛЮБОМ ответе: 200, 400, 403, 404 и 500
// читаются им одинаково. Имя при этом остаётся ПУСТЫМ (первый прогон кейса) либо
// хранит значение ПРЕДЫДУЩЕГО ресурса (повторный проход, соседний кейс), и кейс
// идёт дальше по координате, которой нет.
//
// Падает он через два-три шага — на том, кто сделал ровно то, что положено делать
// при отсутствующем предмете. Наблюдалось 2026-08-12: создание слушателя получило
// отказ в окне материализации прав, а отчёт назвал виновником проверку запрета
// удаления двумя шагами ниже. Атрибуция стоила двух полных прогонов конвейера,
// при том что ответ края лежал в отчёте всё это время.
//
// # Что здесь считается защитой
//
// Наличие утверждения, СПОСОБНОГО УПАСТЬ, в исполняемой части скрипта шага:
// `pm.test(`, `pm.expect(` или `pm.response.to.`. Перечень закрытый — «что-нибудь
// похожее на проверку» засчитывало бы за утверждение сам захват, а захват не
// падает никогда. Тот же перечень и по той же причине уже принят соседним гейтом
// половины удаления (`deploy/scripts/assert-delete-steps-are-asserted.py`) и
// генераторами (`_carries_assertion`).
//
// ФОРМА НЕ НАВЯЗЫВАЕТСЯ, навязывается НАЛИЧИЕ. Требовать здесь именно `200` было
// бы неверно: законный исход у шага бывает не один (идемпотентная подготовка
// принимает и `200`, и `409` — оба означают «предусловие установлено»), а у
// состязательного шага он вообще недетерминирован. Утверждение о СОСТАВЕ исходов
// знает только автор шага; гейт знает лишь то, что оно обязано существовать.
//
// # Почему отрицательные кейсы проходят BY CONSTRUCTION, а не по списку
//
// Шаг, чей ПРЕДМЕТ — отказ, утверждение об этом отказе уже несёт: `oneOf([400,
// 403, 404])`, `assert_grpc_code`, `assert_field_violation`. Значит он проходит
// гейт тем же условием, что и всякий другой, — и списка исключений здесь нет
// вовсе. Список пришлось бы вести руками, он пережил бы свой предмет и стал бы
// местом, куда пропуск вносят незамеченным.
//
// # Шов с соседним гейтом фантомного идентификатора
//
// `newmanphantomid_test.go` держит АСИНХРОННУЮ полосу: операция несёт
// предвыделенный идентификатор в `metadata` даже когда завершилась ошибкой,
// поэтому у неё обязан быть назван ИСХОД. Здесь — СИНХРОННАЯ половина той же
// пары: собственный ответ шага, из которого захват вообще происходит. Они не
// соперники и не дублируют друг друга: там мутация принята краем и провалилась в
// воркере, здесь — не принята вовсе. Разбор у обоих один (`nmDerivedEnvSets`),
// отличается только семя происхождения значения.
//
// # Предмет гейта — СГЕНЕРИРОВАННЫЕ коллекции, а не питоновские исходники
//
// Исполняет newman именно коллекции. Генератор, отступивший от формы, виден здесь
// через свой продукт, и обойти гейт правкой мимо генератора нельзя. Тот же выбор и
// по той же причине сделан в `newmanphantomid_test.go` и
// `newmanprerequestguard_test.go`.
//
// # Предикат читает ИСПОЛНЯЕМУЮ часть, а не текст
//
// Слова `pm.test` и `pm.expect` встречаются в комментариях, ОБЪЯСНЯЮЩИХ эти самые
// утверждения, — и такие комментарии дописывают проходы генератора. Поиск по
// сырому тексту зеленел бы на снятой защите тем вернее, чем лучше она описана
// (`testing.md` §«Гейт на класс», пункт 4). Поэтому обе проекции строит один
// разбор (`jsBlank`): решение принимается по коду, имя переменной читается из
// проекции, где строковые литералы целы.
//
// # Способность упасть доказана инъекцией в ОБЕ стороны
//
// `newmancapturedvar_injection_test.go`: настоящий захват без утверждения краснеет
// и называет координату; рядом — законные близнецы (захват с утверждением успеха,
// захват с утверждением отказа, шаг без захвата), на которых гейт молчит.
package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// nmAssertForms — три формы утверждения postman, каждая СПОСОБНА уронить шаг:
//
//	pm.test('...', () => pm.expect(...))   — именованное утверждение;
//	pm.expect(...)                          — голое, бросает и валит скрипт;
//	pm.response.to.have.status(200)         — форма chai-обёртки postman.
//
// Перечень намеренно закрытый: см. шапку файла.
var nmAssertForms = []string{"pm.test(", "pm.expect(", "pm.response.to."}

// nmCarriesAssertion — в ИСПОЛНЯЕМОЙ части скрипта есть утверждение.
//
// Читается проекция со строками (`jsCodeKeepingStrings`), а не скелет: гейт и
// проходы генератора обязаны считать одно и то же, а `_carries_assertion` в
// генераторах снимает только комментарии. Строгость выше генераторской здесь
// вредна — она развела бы два места об одном предмете.
func nmCarriesAssertion(src string) bool {
	code := jsCodeKeepingStrings(src)
	for _, f := range nmAssertForms {
		if strings.Contains(code, f) {
			return true
		}
	}
	return false
}

// nmCapturedFromResponse — имена окружения, которым шаг присваивает значение,
// происходящее из СВОЕГО ответа (`pm.response`), с учётом области видимости
// промежуточных объявлений.
//
// Сброс имени в пустую строку (`pm.environment.set` с пустым литералом вторым
// аргументом) захватом НЕ является: его значение от ответа не происходит, и
// требовать за него утверждения значило бы ловить форму вместо существа.
func nmCapturedFromResponse(src string) []string {
	return nmDerivedEnvSets(src, "pm.response", nil)
}

// nmCapturedVarFinding — одна находка: шаг, захвативший имя и не сказавший о своём
// исходе ничего.
type nmCapturedVarFinding struct {
	collection string
	casePath   string
	step       string
	method     string
	vars       []string
}

func (f nmCapturedVarFinding) String() string {
	return fmt.Sprintf("%s :: %s :: %s [%s] — захватывает %s и не несёт ни одного утверждения",
		f.collection, f.casePath, f.step, f.method, strings.Join(f.vars, ","))
}

// nmCapturedVarCensus — объём осмотренного. Печатается вместе с вердиктом, чтобы
// «ноль находок» было отличимо от «ноль прочитанного».
type nmCapturedVarCensus struct {
	collections, steps, capturing, asserting int
	byMethod                                 map[string]int
}

func TestCapturedVariableStepCarriesAnAssertion(t *testing.T) {
	root := repoRoot(t)

	// Состав берётся из ИНДЕКСА git, а не обходом диска: под корнем лежат рабочие
	// копии агентов и распаковки отчётов прогонов, и вердикт по ним был бы
	// свойством чужого рабочего каталога, а не коммита (см. trackedtree_test.go).
	tt := newTrackedTree(t, root)
	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)

	findings, cen, err := auditCapturedVarAssertions(root, cols)
	if err != nil {
		t.Fatal(err)
	}

	// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. «Ноль находок» обязано быть отличимо от
	// «ноль прочитанного»: гейт, у которого распознаватель захвата перестал
	// что-либо узнавать (смена формы `save_from_response`, переезд коллекций,
	// переименование поля), молча стал бы вечнозелёным.
	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать")
	}
	if cen.steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов 0 — обход не узнал ни одного шага", cen.collections)
	}
	if cen.capturing == 0 {
		t.Fatalf("в %d шагах не найдено НИ ОДНОГО захвата из собственного ответа — "+
			"предикат захвата ослеп; чинить надо гейт, а не выходить успехом", cen.steps)
	}

	methods := make([]string, 0, len(cen.byMethod))
	for m := range cen.byMethod {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	var perMethod []string
	for _, m := range methods {
		perMethod = append(perMethod, fmt.Sprintf("%s %d", m, cen.byMethod[m]))
	}
	t.Logf("осмотрено: коллекций %d, шагов %d, из них захватывающих из своего ответа %d "+
		"(по методу: %s), несущих утверждение %d",
		cen.collections, cen.steps, cen.capturing, strings.Join(perMethod, ", "), cen.asserting)

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "шагов, захватывающих переменную и не утверждающих ничего: %d\n\n", len(findings))
		b.WriteString("Захват записан так, что отказ его не роняет: тела нет — присваивание не\n")
		b.WriteString("исполняется, разбор глотает catch. Имя остаётся пустым либо хранит значение\n")
		b.WriteString("предыдущего ресурса, и падает не тот шаг, который ошибся.\n\n")
		b.WriteString("Чинится в cases/*.py набора — утверждением об исходе, который автор шага\n")
		b.WriteString("считает законным (assert_status, assert_grpc_code, oneOf по составу исходов),\n")
		b.WriteString("после чего коллекции перегенерируются scripts/gen.py.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}

// auditCapturedVarAssertions — весь разбор одним входом: чтение коллекций, обход
// папок, находки и перепись.
//
// Вынесено из тела гейта намеренно: проба, доказывающая способность гейта упасть,
// обязана гонять ТУ ЖЕ функцию, а не свою копию логики, — иначе она доказывает
// свойство копии (тот же порядок, что у `auditPublishedIdOutcome` по соседству).
func auditCapturedVarAssertions(root string, cols []string) ([]nmCapturedVarFinding, nmCapturedVarCensus, error) {
	var findings []nmCapturedVarFinding
	cen := nmCapturedVarCensus{byMethod: map[string]int{}}

	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		var col nmCollection
		if err := json.Unmarshal(b, &col); err != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, err)
		}
		cen.collections++

		var walk func(items []nmItem, path []string)
		walk = func(items []nmItem, path []string) {
			for _, it := range items {
				if it.isFolder() {
					walk(it.Item, append(path, it.Name))
					continue
				}
				cen.steps++
				src := it.testScript()
				vars := nmCapturedFromResponse(src)
				if len(vars) == 0 {
					continue
				}
				cen.capturing++
				cen.byMethod[it.method()]++
				if nmCarriesAssertion(src) {
					cen.asserting++
					continue
				}
				findings = append(findings, nmCapturedVarFinding{
					collection: rel,
					casePath:   strings.Join(path, " / "),
					step:       it.Name,
					method:     it.method(),
					vars:       vars,
				})
			}
		}
		walk(col.Item, nil)
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].String() < findings[j].String() })
	return findings, cen, nil
}
