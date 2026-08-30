// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// bootguardpresence_injection_test.go — ИНЪЕКЦИЯ обеих осей гейта посадки.
//
// ПОЧЕМУ ОНА ЗАВЕДЕНА ОТДЕЛЬНЫМ АРТЕФАКТОМ. У первой редакции гейта инъекции не
// было вовсе: автор прогнал её разово руками, и круга она не пережила. Цена
// названа замером, а не предположена — внешняя проверка подставила стража,
// сведённого к раннему `return nil`, и гейт вместе со ВСЕМ набором
// `internal/repohygiene` прошёл насквозь (код возврата 0). Утверждение godoc
// «подделать её нельзя, не написав настоящей проверки» было ложным ровно на том
// случае, ради которого гейт заводился.
//
// ОБЕ СТОРОНЫ ПО КАЖДОЙ ОСИ. Без стороны «краснеет» гейт утверждает свойство,
// которого не проверяет; без стороны «молчит» он ловит форму, а не существо, и
// первый же ложный срабат его отключит.
//
// ИНЪЕКЦИЯ ГОНЯЕТ ТЕ ЖЕ ФУНКЦИИ, что и гейт (`scanPostureReach`,
// `scanContractRefusalWitness`), а не их копии: проверка своей копии доказывает
// свойство копии.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── фикстуры оси ДОСТИЖЕНИЯ ────────────────────────────────────────────────

// Объявление посадочных ручек. Форма — та же, что в дереве.
const injPostureKnobsSrc = `package config

type Config struct {
	AuthMode   string ` + "`envconfig:\"KACHO_DEMO_AUTH_MODE\" default:\"production\"`" + `
	DBSSLMode  string ` + "`envconfig:\"KACHO_DEMO_DB_SSLMODE\" default:\"disable\"`" + `
}
`

// Композиционный корень, ДОВОДЯЩИЙ ручки до дескриптора: обе величины уезжают
// наверх, отказ теряться негде.
const injDescriptorAcceptedSrc = `package main

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

func describe(cfg Config) (servicecontract.Descriptor, error) {
	return servicecontract.New(servicecontract.Spec{
		Service:   "kacho-demo",
		DBSSLMode: cfg.DBSSLMode,
	})
}
`

// ЗАГЛУШКА, КОТОРАЯ СОБИРАЕТСЯ, — форма «гашение В ТЕЛЕ С `New`». Дескриптор
// принят, вызов на месте, импорты целы, компилятор доволен, а отказ выброшен в
// `_`. Поиск подстроки её не отличает от исправной:
// `servicecontract.New(servicecontract.Spec{` встречается в обоих случаях
// дословно.
//
// ЕЁ ПРОИЗВОДИТЕЛЯ В ДЕРЕВЕ СЕГОДНЯ НЕТ, и это сказано числом, а не умолчанием.
// Все шесть живых вызовов дескриптора стоят формой `return New(Spec{…})`:
//
//	grep -rn 'servicecontract.New(' --include=*.go services/ gateway/ | grep -v _test.go
//	grep -rn ', _ := servicecontract.New\|, _ = servicecontract.New' --include=*.go services/ gateway/
//
// Первый даёт шесть строк, все — `return`; второй пуст. Форма, гасящая посадку
// НА ЖИВОМ дереве, живёт у ВЫЗЫВАЮЩЕГО поставщика (`desc, _ := describe(…)`), и
// её сторожит ось достижения выхода ниже.
//
// ПОЧЕМУ ЭТА ФИКСТУРА ВСЁ ЖЕ ОСТАЁТСЯ. Ветка `markDiscard` в гейте есть, и
// проверка без неё утверждала бы свойство, которого не проверяет. Но одна она
// доказывала бы гейт на случае, которого не бывает, — то есть была бы допуском
// на исход БЕЗ ПРОИЗВОДИТЕЛЯ, ровно тем классом, который эта линия устранила у
// сквозных проб. Поэтому случаев ДВА, и живой стоит первым.
const injDescriptorDiscardedSrc = `package main

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

func describe(cfg Config) servicecontract.Descriptor {
	desc, _ := servicecontract.New(servicecontract.Spec{
		Service:   "kacho-demo",
		DBSSLMode: cfg.DBSSLMode,
	})
	return desc
}
`

// Правдоподобный ЛОЖНЫЙ близнец: локальный страж есть, позван, доменных проверок
// полно — но до центрального дескриптора ручки не доезжают. По первой редакции
// гейта этого было ДОСТАТОЧНО; по нынешней ось развёрнута, и это находка.
const injLocalGuardOnlySrc = `package main

import "fmt"

func (c Config) Validate() error {
	if c.DBSSLMode == "disable" {
		return fmt.Errorf("production mode refuses insecure config: %s", c.DBSSLMode)
	}
	return nil
}

func main() {
	var cfg Config
	if err := cfg.Validate(); err != nil {
		panic(err)
	}
}
`

// ── направление (а): ручки объявлены, дескриптор не принят ─────────────────

func TestPostureReachGateRedWhenKnobsNeverReachTheDescriptor(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/demo/internal/config/config.go": injPostureKnobsSrc,
		"services/demo/cmd/demo/main.go":          injLocalGuardOnlySrc,
	})
	reach, err := scanPostureReach(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if reach.filesRead == 0 {
		t.Fatal("синтетическое дерево не прочитано — вердикт был бы о пустоте")
	}
	if len(reach.knobs["demo"]) == 0 {
		t.Fatalf("посадочные ручки не распознаны: %v", reach.knobs)
	}
	if reach.accepts["demo"] != "" {
		t.Fatalf("дескриптор засчитан там, где его нет: %q", reach.accepts["demo"])
	}
	// Находка обязана НАЗЫВАТЬ компонент: без имени по ней нечего чинить.
	if _, ok := postureReachRelaxations["demo"]; ok {
		t.Fatal("фикстура столкнулась с настоящей ведомостью — имя компонента занято")
	}
	t.Logf("(а) красный: ручки %v, принятий дескриптора 0", reach.knobs["demo"])
}

// ── направление (б): законный близнец — дескриптор принят, гейт молчит ─────

func TestPostureReachGateSilentWhenTheDescriptorIsAccepted(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/demo/internal/config/config.go": injPostureKnobsSrc,
		"services/demo/cmd/demo/describe.go":      injDescriptorAcceptedSrc,
	})
	reach, err := scanPostureReach(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if len(reach.knobs["demo"]) == 0 {
		t.Fatalf("посадочные ручки не распознаны: %v", reach.knobs)
	}
	if reach.accepts["demo"] == "" {
		t.Fatal("законное принятие дескриптора НЕ засчитано — гейт ловит форму, а не " +
			"существо, и покраснел бы на исправном дереве")
	}
	if reach.discards["demo"] != "" {
		t.Fatalf("исправное принятие названо выброшенным отказом: %q", reach.discards["demo"])
	}
}

// Композиционный корень ЖИВОЙ формы: поставщик отдаёт дескриптор `return`-ом, а
// гасит отказ ВЫЗЫВАЮЩИЙ. Именно так устроены все шесть компонентов дерева, и
// именно эта форма гасит посадку одним символом.
const injDescriptorQuenchedAtCallerSrc = `package main

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

func describe(cfg Config) (servicecontract.Descriptor, error) {
	return servicecontract.New(servicecontract.Spec{
		Service:   "kacho-demo",
		DBSSLMode: cfg.DBSSLMode,
	})
}

func runServe(cfg Config) error {
	desc, _ := describe(cfg)
	_ = desc
	return nil
}
`

// ── ЗАГЛУШКА, КОТОРАЯ СОБИРАЕТСЯ: отказ дескриптора не доходит до остановки ──
//
// СЛУЧАЕВ ДВА, И ПЕРВЫЙ — ЖИВОЙ. Прежняя редакция этой пробы знала только
// второй — гашение в теле с `New`, — а его производителя в дереве НОЛЬ (предикат
// назван у фикстуры). То есть проверка перечисляла случай, которого не бывает, и
// потому ничего не говорила о форме, которая бывает: подделка у вызывающего
// проходила и её, и весь набор `internal/repohygiene`.
func TestPostureReachGateRedWhenTheDescriptorRefusalIsDiscarded(t *testing.T) {
	t.Run("живая форма: гашение у ВЫЗЫВАЮЩЕГО поставщика", func(t *testing.T) {
		root := synthCarrierTree(t, map[string]string{
			"services/demo/internal/config/config.go": injPostureKnobsSrc,
			"services/demo/cmd/demo/describe.go":      injDescriptorQuenchedAtCallerSrc,
		})
		reach, err := scanPostureReach(root)
		if err != nil {
			t.Fatalf("обход синтетического дерева: %v", err)
		}
		// Дескриптор здесь принят ЗАКОННО — и это существо случая: обе прежние
		// оси зелены, а посадка не проверяется ничем.
		if reach.accepts["demo"] == "" {
			t.Fatal("законное принятие дескриптора не засчитано — проба проверяла бы " +
				"не ту ось")
		}
		if reach.discards["demo"] != "" {
			t.Fatalf("ветка гашения В ТЕЛЕ засчитана там, где гасит вызывающий: %q",
				reach.discards["demo"])
		}
		if len(reach.quenched["demo"]) == 0 {
			t.Fatal("гашение у вызывающего НЕ распознано: страж собран, исполняется и " +
				"не может ничего остановить, а обе прежние оси при этом зелены")
		}
		if reach.callersSeen == 0 || reach.callersReach != 0 {
			t.Fatalf("перепись не показала разрыв: осмотрено %d, доходит %d",
				reach.callersSeen, reach.callersReach)
		}
		t.Logf("живая форма распознана: %s", strings.Join(reach.quenched["demo"], "; "))
	})

	t.Run("форма без производителя в дереве: гашение в теле с New", func(t *testing.T) {
		root := synthCarrierTree(t, map[string]string{
			"services/demo/internal/config/config.go": injPostureKnobsSrc,
			"services/demo/cmd/demo/describe.go":      injDescriptorDiscardedSrc,
		})
		reach, err := scanPostureReach(root)
		if err != nil {
			t.Fatalf("обход синтетического дерева: %v", err)
		}
		if reach.accepts["demo"] != "" {
			t.Fatalf("выброшенный отказ засчитан за принятие: %q — гейт зеленеет на "+
				"заглушке, которая собирается", reach.accepts["demo"])
		}
		if reach.discards["demo"] == "" {
			t.Fatal("выброшенный в `_` отказ не распознан: находка потерялась бы целиком")
		}
		t.Logf("заглушка распознана: %s", reach.discards["demo"])
	})
}

// ── послабление, которому НЕЧЕГО ИСКЛЮЧАТЬ ────────────────────────────────

// Ведомость обязана истекать сама. Проба гоняет ТУ ЖЕ ветку решения, что и гейт,
// на синтетической ведомости: запись против компонента, который дескриптор УЖЕ
// принимает, — находка, а не тишина.
func TestPostureRelaxationExpiresWhenItsSubjectIsGone(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/demo/internal/config/config.go": injPostureKnobsSrc,
		"services/demo/cmd/demo/describe.go":      injDescriptorAcceptedSrc,
	})
	reach, err := scanPostureReach(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	ledger := map[string]postureReachRelaxation{
		"demo":  {issue: 1, why: "предмет исчез: компонент принял дескриптор"},
		"ghost": {issue: 2, why: "такого компонента в дереве нет"},
	}
	findings := adjudicatePostureReach(reach, ledger)
	joined := strings.Join(findings, "\n")
	if !strings.Contains(joined, "demo") || !strings.Contains(joined, "НЕЧЕГО ИСКЛЮЧАТЬ") {
		t.Fatalf("послабление, которому нечего исключать, не стало находкой:\n%s", joined)
	}
	if !strings.Contains(joined, "ghost") {
		t.Fatalf("запись о несуществующем компоненте пережила свой предмет "+
			"незамеченной:\n%s", joined)
	}

	// Обратная сторона: пока предмет ЕСТЬ, послабление молчит — иначе ведомость
	// нельзя было бы завести вовсе.
	root2 := synthCarrierTree(t, map[string]string{
		"services/demo/internal/config/config.go": injPostureKnobsSrc,
		"services/demo/cmd/demo/main.go":          injLocalGuardOnlySrc,
	})
	reach2, err := scanPostureReach(root2)
	if err != nil {
		t.Fatalf("обход второго синтетического дерева: %v", err)
	}
	live := adjudicatePostureReach(reach2, map[string]postureReachRelaxation{
		"demo": {issue: 1, why: "предмет ещё есть"},
	})
	if len(live) != 0 {
		t.Fatalf("послабление с живым предметом объявлено истёкшим:\n%s",
			strings.Join(live, "\n"))
	}
}

// ── ПУСТОЙ ОБХОД — ОТКАЗ, А НЕ УСПЕХ ──────────────────────────────────────

func TestPostureReachRefusesAnEmptyTraversal(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"README.md": "дерево без единого компонента\n",
	})
	reach, err := scanPostureReach(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if reach.filesRead != 0 {
		t.Fatalf("в дереве без компонентов прочитано %d файлов", reach.filesRead)
	}
	// Гейт на такой переписи обязан ОТКАЗАТЬ: «ноль находок» здесь неотличимо от
	// «ноль прочитанного», и молчаливый успех означал бы «всё в порядке» ровно
	// там, где не проверено ничего.
	if len(reach.components) != 0 {
		t.Fatalf("компоненты найдены в дереве, где их нет: %v", reach.components)
	}
}

// ── фикстуры оси СВИДЕТЕЛЯ ────────────────────────────────────────────────

// НАСТОЯЩИЙ свидетель, форма «присваивание отдельной строкой» — та самая, что
// живёт в дереве.
const injWitnessAssignSrc = `package servicecontract_test

import "testing"

func TestRefusesWeakSSLMode(t *testing.T) {
	s := lawful()
	s.DBSSLMode = "disable"
	_, err := servicecontract.New(s)
	if err == nil {
		t.Fatalf("дескриптор принят — отказ не способен упасть")
	}
}
`

// ЛОЖНЫЙ СВИДЕТЕЛЬ, естественный близнец — и это главная сторона инъекции.
// В файле ЕСТЬ вызов `servicecontract.New` и ЕСТЬ `require.Error(` — но на
// РАЗНЫХ ошибках: первая проба принимает законный дескриптор (положительный
// контроль, полезный сам по себе), вторая ждёт ошибки от чужого разбора.
// Гейт, признающий свидетеля ПО ФАЙЛУ, зеленел бы здесь; гейт, читающий тело,
// не признаёт.
const injWitnessDecoySrc = `package servicecontract_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLawfulSpecIsAccepted(t *testing.T) {
	if _, err := servicecontract.New(lawful()); err != nil {
		t.Fatalf("законный дескриптор отвергнут: %v", err)
	}
}

func TestParseModeRejectsGarbage(t *testing.T) {
	_, perr := servicecontract.ParseMode("не-режим")
	require.Error(t, perr)
}
`

// ВТОРАЯ ПОДДЕЛКА, и она изолирует ДРУГУЮ ось. Первая (`injWitnessDecoySrc`)
// разносит вызов и утверждение по РАЗНЫМ телам — её отсекает пофункциональный
// обход. Эта держит оба в ОДНОМ теле: `New` связан с `err`, а утверждение стоит
// на `perr` — ошибке чужого разбора. Отсекает её только привязка утверждения к
// РЕЗУЛЬТАТУ ТОГО ЖЕ вызова.
//
// Две подделки нужны порознь: инъекция, роняющая сразу обе оси, не показывает,
// какая из них жива (`testing.md` §«Гейт на класс», п.2в).
const injWitnessWrongErrSrc = `package servicecontract_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefusesSomething(t *testing.T) {
	s := lawful()
	_, err := servicecontract.New(s)
	_, perr := servicecontract.ParseMode("не-режим")
	require.Error(t, perr)
	_ = err
}
`

// Прочие ЗАКОННЫЕ формы записи свидетеля. Форма, о которой распознаватель не
// знает, не даёт ни красного, ни зелёного — она молчит, и всё записанное в ней
// оказывается вне наблюдения (`testing.md` §«Гейт на класс», п.7).
const injWitnessInlineIfSrc = `package servicecontract_test

import "testing"

func TestRefusesInline(t *testing.T) {
	s := lawful()
	s.Forwarders = nil
	if _, err := servicecontract.New(s); err == nil {
		t.Fatal("дескриптор принят")
	}
}
`

const injWitnessRequireErrorSrc = `package servicecontract_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefusesViaRequire(t *testing.T) {
	s := lawful()
	s.DBSSLMode = "disable"
	_, err := servicecontract.New(s)
	require.Error(t, err)
}
`

const injWitnessAssertErrorSrc = `package servicecontract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRefusesViaAssert(t *testing.T) {
	s := lawful()
	s.Forwarders = nil
	_, err := servicecontract.New(s)
	assert.Error(t, err)
}
`

const injWitnessErrorsIsSrc = `package servicecontract_test

import (
	"errors"
	"testing"
)

func TestRefusesWithNamedError(t *testing.T) {
	s := lawful()
	s.DBSSLMode = "disable"
	_, err := servicecontract.New(s)
	if !errors.Is(err, servicecontract.ErrPosture) {
		t.Fatal("не тот вид отказа")
	}
}
`

// Свидетель ПО ДЕЛЕГАЦИИ: вызов идёт через хелпер — форма, которой пользуется
// весь пакет контракта.
const injWitnessDelegatedSrc = `package servicecontract_test

import "testing"

func refuses(t *testing.T, s servicecontract.Spec) {
	t.Helper()
	_, err := servicecontract.New(s)
	if err == nil {
		t.Fatalf("дескриптор принят")
	}
}

func TestRefusesForwarders(t *testing.T) {
	s := lawful()
	s.Forwarders = nil
	refuses(t, s)
}

func TestRefusesSSLMode(t *testing.T) {
	s := lawful()
	s.DBSSLMode = "disable"
	refuses(t, s)
}
`

// synthWitnessDir отдаёт ПУТИ написанных файлов, а не каталог.
//
// Состав синтетического пакета известен здесь точно — он только что записан, —
// поэтому спрашивать его повторно (у диска ли, у индекса ли) незачем: разбору
// передаётся то, что создано. Индекса у временного каталога нет и быть не
// может, а обход диска на этом месте пришлось бы отличать от обхода настоящего
// дерева, где он запрещён.
func synthWitnessDir(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, 0, len(files))
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", name, err)
		}
		// Отбор тот же, что делал образец `*_test.go` внутри разбора: не-проба
		// (README пустого пакета) в состав не идёт. Без этой строки разбор
		// получил бы файл, который Go не является, и пустой пакет перестал бы
		// быть пустым — то есть фикстура «проб нет» проверяла бы не то.
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, full)
	}
	return paths
}

// ── направление (а): свидетеля НЕТ, хотя файл выглядит как свидетельство ───

func TestRefusalWitnessGateRedOnAFileLevelDecoy(t *testing.T) {
	paths := synthWitnessDir(t, map[string]string{"decoy_test.go": injWitnessDecoySrc})
	w, err := scanContractRefusalWitness(paths)
	if err != nil {
		t.Fatalf("разбор синтетического пакета: %v", err)
	}
	if w.funcsRead == 0 {
		t.Fatal("синтетический пакет не прочитан")
	}
	if len(w.direct) != 0 {
		t.Fatalf("ЛОЖНЫЙ свидетель засчитан: %v. В файле есть и вызов `New`, и "+
			"`require.Error(` — но на РАЗНЫХ ошибках; признание по файлу зеленело бы "+
			"на подделке", w.direct)
	}
	if len(w.delegating) != 0 {
		t.Fatalf("делегация засчитана в отсутствие прямого свидетеля: %v", w.delegating)
	}
}

// Подделка в ОДНОМ теле: утверждение на чужой ошибке.
func TestRefusalWitnessGateRedWhenTheAssertionRidesAnotherError(t *testing.T) {
	paths := synthWitnessDir(t, map[string]string{"w_test.go": injWitnessWrongErrSrc})
	w, err := scanContractRefusalWitness(paths)
	if err != nil {
		t.Fatalf("разбор синтетического пакета: %v", err)
	}
	if w.funcsRead == 0 {
		t.Fatal("синтетический пакет не прочитан")
	}
	if len(w.direct) != 0 {
		t.Fatalf("свидетелем засчитано тело, где утверждение стоит на ЧУЖОЙ ошибке: %v. "+
			"Вызов `New` и `require.Error(` в одном теле — ещё не свидетельство", w.direct)
	}
}

// ── направление (б): каждая ЗАКОННАЯ форма признаётся ─────────────────────

func TestRefusalWitnessGateKnowsEveryLawfulForm(t *testing.T) {
	forms := []struct {
		name string
		src  string
		fn   string
	}{
		{"присваивание отдельной строкой", injWitnessAssignSrc, "TestRefusesWeakSSLMode"},
		{"единым выражением в `if`", injWitnessInlineIfSrc, "TestRefusesInline"},
		{"require.Error", injWitnessRequireErrorSrc, "TestRefusesViaRequire"},
		{"assert.Error", injWitnessAssertErrorSrc, "TestRefusesViaAssert"},
		{"errors.Is на результате", injWitnessErrorsIsSrc, "TestRefusesWithNamedError"},
	}
	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			paths := synthWitnessDir(t, map[string]string{"w_test.go": f.src})
			w, err := scanContractRefusalWitness(paths)
			if err != nil {
				t.Fatalf("разбор: %v", err)
			}
			if len(w.direct) == 0 {
				t.Fatalf("законная форма %q НЕ признана свидетелем — всё записанное в "+
					"ней оказалось бы вне наблюдения", f.name)
			}
			if w.direct[0] != f.fn {
				t.Fatalf("свидетелем назван %q вместо %q", w.direct[0], f.fn)
			}
		})
	}
}

// Делегация на один уровень — та форма, которой пользуется настоящий пакет.
func TestRefusalWitnessGateFollowsOneLevelOfDelegation(t *testing.T) {
	paths := synthWitnessDir(t, map[string]string{"w_test.go": injWitnessDelegatedSrc})
	w, err := scanContractRefusalWitness(paths)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(w.direct) != 1 || w.direct[0] != "refuses" {
		t.Fatalf("прямой свидетель не найден: %v", w.direct)
	}
	if len(w.delegating) != 2 {
		t.Fatalf("свидетелей по делегации %d, ожидалось 2: %v", len(w.delegating), w.delegating)
	}
	// Оси, тронутые делегирующими телами, обязаны доехать до переписи: без этого
	// гейт не мог бы утверждать, что снятая копия стража geo ничего не потеряла.
	for _, axis := range []string{"DBSSLMode", "Forwarders"} {
		if !w.axes[axis] {
			t.Fatalf("ось %s не собрана с делегирующего свидетеля: %v", axis, w.axes)
		}
	}
}

// ПОЛЯРНОСТЬ. `err != nil` — это ПОЛОЖИТЕЛЬНЫЙ контроль («законное принято»), и
// свидетелем ОТКАЗА он не является. Без этой пробы гейт зеленел бы на пакете,
// где дескриптор не отвергает ничего, а пробы лишь подтверждают приём.
func TestRefusalWitnessGateDoesNotMistakeAcceptanceForRefusal(t *testing.T) {
	const acceptOnly = `package servicecontract_test

import "testing"

func TestLawfulIsAccepted(t *testing.T) {
	s := lawful()
	_, err := servicecontract.New(s)
	if err != nil {
		t.Fatalf("законный дескриптор отвергнут: %v", err)
	}
}
`
	paths := synthWitnessDir(t, map[string]string{"w_test.go": acceptOnly})
	w, err := scanContractRefusalWitness(paths)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(w.direct) != 0 {
		t.Fatalf("положительный контроль засчитан за свидетеля отказа: %v", w.direct)
	}
}

// ПУСТОЙ ПАКЕТ — перепись обязана это показать, а гейт выше — отказать.
func TestRefusalWitnessRefusesAnEmptyPackage(t *testing.T) {
	paths := synthWitnessDir(t, map[string]string{"README.md": "проб нет\n"})
	w, err := scanContractRefusalWitness(paths)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if w.funcsRead != 0 {
		t.Fatalf("в пакете без проб прочитано %d функций", w.funcsRead)
	}
}

// ── фикстуры оси ПРОВЯЗКИ: значение ручки доходит до стража ────────────────
//
// ПОДДЕЛКА ЗДЕСЬ ОБЯЗАНА БЫТЬ ТИХОЙ. Негодная константа (пустая строка,
// заведомо слабый режим) уронила бы старт ГРОМКО — дескриптор её отвергает, — и
// такая инъекция доказывала бы не то: она проверяла бы дескриптор, а не гейт.
// Годная подделка — ПРАВДОПОДОБНАЯ константа: `"require"`, `false`,
// `ModeProduction`. Страж на ней доволен всегда, ручка не действует никогда, а
// строка выглядит настройкой.

// Шапка фикстуры: объявление ручек плюс сборщик дескриптора. `%s` — тело
// посадочных полей, единственное, чем стороны инъекции различаются.
const injWiringTmpl = `package main

import (
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
)

type Config struct {
	AuthMode  string ` + "`envconfig:\"KACHO_DEMO_AUTH_MODE\"`" + `
	DBSSLMode string ` + "`envconfig:\"KACHO_DEMO_DB_SSLMODE\"`" + `
	OptIn     bool   ` + "`envconfig:\"KACHO_DEMO_AUTHZ_TRUST_ANY_FORWARDER\"`" + `
	Nested    struct{ URL string }
}

func (c Config) TrustedForwarders() grpcsrv.TrustedForwarders { return nil }
func (c Config) DSN() string                                  { return "" }

func describe(cfg Config) (servicecontract.Descriptor, error) {
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return servicecontract.Descriptor{}, err
	}
	_ = coredb.SSLModeFromDSN
	return servicecontract.New(servicecontract.Spec{
		Service: "kacho-demo",
%s
	})
}
`

func injWiring(body string) string { return fmt.Sprintf(injWiringTmpl, body) }

// Провязано целиком — законный близнец КАЖДОЙ подделки ниже.
const injWiredBody = `		Mode:      mode,
		DBSSLMode: cfg.DBSSLMode,
		Forwarders: cfg.TrustedForwarders(),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "KACHO_DEMO_AUTHZ_TRUSTED_FORWARDER_SANS",
			TrustAny: "KACHO_DEMO_AUTHZ_TRUST_ANY_FORWARDER",
			OptIn:    cfg.OptIn,
		},`

func scanWiring(t *testing.T, body string) postureReach {
	t.Helper()
	root := synthCarrierTree(t, map[string]string{
		"services/demo/cmd/demo/describe.go": injWiring(body),
	})
	reach, err := scanPostureReach(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if reach.accepts["demo"] == "" {
		t.Fatalf("фикстура не принимает дескриптор — инъекция проверяла бы не ту ось")
	}
	if reach.fieldsSeen == 0 {
		t.Fatal("посадочных полей не распознано — «ноль находок» здесь не утверждает ничего")
	}
	return reach
}

// ЗАКОННЫЙ БЛИЗНЕЦ: всё выведено из конфигурации — гейт МОЛЧИТ.
// Стоит первым намеренно: пока он красный, каждая подделка ниже краснеет по
// причине, не имеющей отношения к своему предмету.
func TestSpecWiringSilentWhenEveryPostureFieldComesFromConfig(t *testing.T) {
	reach := scanWiring(t, injWiredBody)
	if len(reach.literalFields["demo"]) != 0 {
		t.Fatalf("исправная провязка объявлена находкой — гейт краснел бы на "+
			"живом дереве:\n%s", strings.Join(reach.literalFields["demo"], "\n"))
	}
	if reach.fieldsSeen != reach.fieldsWired {
		t.Fatalf("осмотрено %d, провязано %d — величины разошлись на исправной "+
			"фикстуре", reach.fieldsSeen, reach.fieldsWired)
	}
	t.Logf("законный близнец: осмотрено %d, провязано %d", reach.fieldsSeen, reach.fieldsWired)
}

// ТИХИЕ ПОДДЕЛКИ — по одной на каждое посадочное поле. Каждая собирается и
// каждая правдоподобна.
func TestSpecWiringRedOnAQuietConstantInEveryPostureField(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		field string
		value string
	}{
		{
			name: "DBSSLMode — годная константа вместо ручки",
			body: strings.Replace(injWiredBody, "DBSSLMode: cfg.DBSSLMode,",
				`DBSSLMode: "require",`, 1),
			field: "DBSSLMode", value: `"require"`,
		},
		{
			name: "Mode — боевой режим назначен на месте",
			body: strings.Replace(injWiredBody, "Mode:      mode,",
				"Mode:      servicecontract.ModeProduction,", 1),
			field: "Mode", value: "servicecontract.ModeProduction",
		},
		{
			name: "Forwarders — круг вшит в корень",
			body: strings.Replace(injWiredBody, "Forwarders: cfg.TrustedForwarders(),",
				`Forwarders: grpcsrv.NewTrustedForwarders("spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"),`, 1),
			field: "Forwarders", value: "grpcsrv.NewTrustedForwarders",
		},
		{
			name: "ForwarderKnobs.OptIn — безопасное значение назначено на месте",
			body: strings.Replace(injWiredBody, "OptIn:    cfg.OptIn,",
				"OptIn:    false,", 1),
			field: "ForwarderKnobs.OptIn", value: "false",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.body == injWiredBody {
				t.Fatal("подстановка не сработала — инъекция была бы тождественной")
			}
			reach := scanWiring(t, c.body)
			lits := strings.Join(reach.literalFields["demo"], "\n")
			if lits == "" {
				t.Fatalf("тихая подделка в %s НЕ найдена: страж доволен всегда, "+
					"ручка не действует никогда, и гейт этого не видит", c.field)
			}
			if !strings.Contains(lits, c.field) {
				t.Fatalf("находка не называет поле %s — по ней нечего чинить:\n%s",
					c.field, lits)
			}
			if !strings.Contains(lits, c.value) {
				t.Fatalf("находка не называет ПОДСТАВЛЕННОЕ значение %s — читателю "+
					"пришлось бы искать самому:\n%s", c.value, lits)
			}
			if reach.fieldsWired >= reach.fieldsSeen {
				t.Fatalf("перепись не показала разрыв: осмотрено %d, провязано %d",
					reach.fieldsSeen, reach.fieldsWired)
			}
		})
	}
}

// ЗАКОННЫЕ ФОРМЫ ПРОВЯЗКИ, каждая порознь. Форма, о которой распознаватель не
// знает, уходит в НЕВИДИМОСТЬ либо в ложную находку — второе случилось на живом
// дереве при первой редакции этой оси.
func TestSpecWiringKnowsEveryLawfulDerivation(t *testing.T) {
	forms := []struct {
		name string
		body string
	}{
		{"поле конфигурации", injWiredBody},
		{"вложенное поле", strings.Replace(injWiredBody, "DBSSLMode: cfg.DBSSLMode,",
			"DBSSLMode: cfg.Nested.URL,", 1)},
		{"чужой вызов с доводом из конфигурации", strings.Replace(injWiredBody,
			"DBSSLMode: cfg.DBSSLMode,", "DBSSLMode: coredb.SSLModeFromDSN(cfg.DSN()),", 1)},
	}
	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			reach := scanWiring(t, f.body)
			if len(reach.literalFields["demo"]) != 0 {
				t.Fatalf("законная форма %q объявлена находкой:\n%s", f.name,
					strings.Join(reach.literalFields["demo"], "\n"))
			}
		})
	}
}

// Промежуточная переменная — цепочкой, а не одним шагом.
func TestSpecWiringFollowsAChainOfIntermediates(t *testing.T) {
	src := `package main

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

type Config struct {
	AuthMode  string ` + "`envconfig:\"KACHO_DEMO_AUTH_MODE\"`" + `
	DBSSLMode string ` + "`envconfig:\"KACHO_DEMO_DB_SSLMODE\"`" + `
}

func (c Config) DSN() string { return "" }

func describe(cfg Config) (servicecontract.Descriptor, error) {
	dsn := cfg.DSN()
	ssl := sslOf(dsn)
	mode, _ := servicecontract.ParseMode(cfg.AuthMode)
	return servicecontract.New(servicecontract.Spec{
		Service:   "kacho-demo",
		Mode:      mode,
		DBSSLMode: ssl,
	})
}

func sslOf(string) string { return "" }
`
	root := synthCarrierTree(t, map[string]string{"services/demo/cmd/demo/d.go": src})
	reach, err := scanPostureReach(root)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(reach.literalFields["demo"]) != 0 {
		t.Fatalf("цепочка промежуточных переменных не распознана:\n%s",
			strings.Join(reach.literalFields["demo"], "\n"))
	}
	if reach.fieldsWired != 2 {
		t.Fatalf("провязанных полей %d, ожидалось 2", reach.fieldsWired)
	}
}

// ── форма «значение приходит ПАРАМЕТРОМ»: судится у вызывающего ────────────

const injParamTmpl = `package main

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

type Config struct {
	AuthMode  string ` + "`envconfig:\"KACHO_DEMO_AUTH_MODE\"`" + `
	DBSSLMode string ` + "`envconfig:\"KACHO_DEMO_DB_SSLMODE\"`" + `
}

%s

func describe(cfg Config, mode servicecontract.Mode) (servicecontract.Descriptor, error) {
	return servicecontract.New(servicecontract.Spec{
		Service:   "kacho-demo",
		Mode:      mode,
		DBSSLMode: cfg.DBSSLMode,
	})
}
`

func TestSpecWiringResolvesAParameterAtItsCaller(t *testing.T) {
	cases := []struct {
		name    string
		caller  string
		finding bool
		unjudge int
	}{
		{
			name: "вызывающий подаёт разобранную ручку — молчит",
			caller: `func runServe(cfg Config) error {
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return err
	}
	_, err = describe(cfg, mode)
	return err
}`,
			finding: false,
		},
		{
			name: "вызывающий подаёт ТИХУЮ константу — находка",
			caller: `func runServe(cfg Config) error {
	_, err := describe(cfg, servicecontract.ModeProduction)
	return err
}`,
			finding: true,
		},
		{
			name:    "вызывающих нет вовсе — судить нечем, идёт в перепись",
			caller:  ``,
			finding: false,
			unjudge: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := synthCarrierTree(t, map[string]string{
				"services/demo/cmd/demo/d.go": fmt.Sprintf(injParamTmpl, c.caller),
			})
			reach, err := scanPostureReach(root)
			if err != nil {
				t.Fatalf("обход: %v", err)
			}
			lits := strings.Join(reach.literalFields["demo"], "\n")
			if c.finding && !strings.Contains(lits, "Mode") {
				t.Fatalf("подставленный у вызывающего режим НЕ найден — форма "+
					"«значение приходит параметром» осталась вне наблюдения:\n%s", lits)
			}
			if !c.finding && lits != "" {
				t.Fatalf("законная форма объявлена находкой:\n%s", lits)
			}
			if reach.fieldsUnjudged != c.unjudge {
				t.Fatalf("«не судится» %d, ожидалось %d — величина обязана быть "+
					"отличима и от провязанного, и от находки",
					reach.fieldsUnjudged, c.unjudge)
			}
		})
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ ЗАКРЫТОГО ПЕРЕЧНЯ: `ForwarderKnobs.SANs` и `.TrustAny` —
// ИМЕНА ручек, а не их значения. Они ОБЯЗАНЫ быть литералами (иначе текст отказа
// не назовёт оператору, что править), и гейт на них молчит by construction.
// Без этой пробы закрытость перечня держалась бы только комментарием.
func TestSpecWiringStaysSilentOnKnobNamesWhichAreRightlyLiteral(t *testing.T) {
	body := strings.Replace(injWiredBody,
		`SANs:     "KACHO_DEMO_AUTHZ_TRUSTED_FORWARDER_SANS",`,
		`SANs:     "СОВСЕМ ДРУГОЕ ИМЯ",`, 1)
	if body == injWiredBody {
		t.Fatal("подстановка не сработала — проба была бы тождественной")
	}
	reach := scanWiring(t, body)
	if len(reach.literalFields["demo"]) != 0 {
		t.Fatalf("имя ручки объявлено находкой — перечень посадочных полей "+
			"разошёлся со своим обоснованием:\n%s",
			strings.Join(reach.literalFields["demo"], "\n"))
	}
}

// ── ИНЪЕКЦИЯ ОСИ «ОТКАЗ ПОСТАВЩИКА ДОХОДИТ ДО ВЫХОДА» ──────────────────────
//
// ПОЧЕМУ ОСЬ ЗАВЕДЕНА ОТДЕЛЬНО ОТ ДВУХ ПРЕДЫДУЩИХ. Ось принятия судит, стоит ли
// вызов дескриптора в корне; ось провязки — доехало ли до него значение ручки.
// Между ними живёт форма, гасящая посадку ОДНИМ символом у ВЫЗЫВАЮЩЕГО:
//
//	desc, _ := describe(cfg, …)   // вместо `desc, err := …; if err != nil {…}`
//
// Она собирается, `go vet` на ней молчит, `errcheck` присваивание в `_` по
// умолчанию не судит. Проверено опытом на живом дереве: до этой оси такая
// подделка проходила ВЕСЬ набор `internal/repohygiene` (код возврата 0), а
// перепись гейта не менялась ни на единицу.
//
// ФОРМ ЗАПИСИ ПРЕДМЕТА НЕСКОЛЬКО, И РАСПОЗНАВАТЕЛЬ ОБЯЗАН ЗНАТЬ ВСЕ
// (`testing.md` §«Гейт на класс», п.7) — по инъекции на каждую, с законным
// близнецом рядом. Форма, о которой он не знает, не даёт ни красного, ни
// зелёного: она молчит.

// injReachTmpl — шапка фикстуры: объявление ручек плюс ПОСТАВЩИК дескриптора.
// `%s` — тело вызывающего, единственное, чем стороны инъекции различаются.
const injReachTmpl = `package main

import (
	"fmt"
	"log"
	"os"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

type Config struct {
	AuthMode  string ` + "`envconfig:\"KACHO_DEMO_AUTH_MODE\"`" + `
	DBSSLMode string ` + "`envconfig:\"KACHO_DEMO_DB_SSLMODE\"`" + `
}

func describe(cfg Config) (servicecontract.Descriptor, error) {
	return servicecontract.New(servicecontract.Spec{
		Service:   "kacho-demo",
		DBSSLMode: cfg.DBSSLMode,
	})
}

var _ = fmt.Sprintf
var _ = log.Printf
var _ = os.Exit

%s
`

// scanReach — обход синтетического дерева с проверкой ПРЕДПОСЫЛКИ инъекции:
// поставщик обязан быть найден, иначе всякая сторона ниже зеленела бы по
// причине, не имеющей отношения к своему предмету.
func scanReach(t *testing.T, caller string) postureReach {
	t.Helper()
	root := synthCarrierTree(t, map[string]string{
		"services/demo/cmd/demo/d.go": fmt.Sprintf(injReachTmpl, caller),
	})
	reach, err := scanPostureReach(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if reach.accepts["demo"] == "" {
		t.Fatal("фикстура не принимает дескриптор — инъекция проверяла бы не ту ось")
	}
	if reach.providersSeen == 0 {
		t.Fatal("поставщик дескриптора не распознан — «ноль находок» здесь не " +
			"утверждает ничего")
	}
	return reach
}

// ЗАКОННЫЕ ФОРМЫ — гейт МОЛЧИТ. Стоят первыми намеренно: пока законный близнец
// красный, каждая подделка ниже краснеет не по своему предмету.
func TestRefusalReachKnowsEveryLawfulHandlingForm(t *testing.T) {
	forms := []struct {
		name   string
		calls  int // вызовов поставщика в фикстуре — перепись обязана назвать РОВНО столько
		caller string
	}{
		{"проверка с возвратом — форма, живущая в дереве", 1, `func runServe(cfg Config) error {
	desc, err := describe(cfg)
	if err != nil {
		return err
	}
	_ = desc
	return nil
}`},
		{"проверка с обёрткой ошибки", 1, `func runServe(cfg Config) error {
	desc, err := describe(cfg)
	if err != nil {
		return fmt.Errorf("describe kacho-demo: %w", err)
	}
	_ = desc
	return nil
}`},
		{"проверка единым выражением", 1, `func runServe(cfg Config) error {
	if _, err := describe(cfg); err != nil {
		return err
	}
	return nil
}`},
		{"остановка процесса через log.Fatalf", 1, `func runServe(cfg Config) {
	desc, err := describe(cfg)
	if err != nil {
		log.Fatalf("describe: %v", err)
	}
	_ = desc
}`},
		{"остановка процесса через os.Exit", 1, `func runServe(cfg Config) {
	desc, err := describe(cfg)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_ = desc
}`},
		{"остановка процесса через panic", 1, `func runServe(cfg Config) {
	desc, err := describe(cfg)
	if err != nil {
		panic(err)
	}
	_ = desc
}`},
		{"голая передача отказа наверх", 1, `func runServe(cfg Config) error {
	_, err := describe(cfg)
	return err
}`},
		{"передача результата целиком", 2, `func build(cfg Config) (servicecontract.Descriptor, error) {
	return describe(cfg)
}

func runServe(cfg Config) error {
	_, err := build(cfg)
	return err
}`},
		{"выход в ветке else", 1, `func runServe(cfg Config) error {
	desc, err := describe(cfg)
	if err == nil {
		_ = desc
	} else {
		return err
	}
	return nil
}`},
		// ── БЛИЗНЕЦЫ ИЗ НАСТОЯЩЕЙ ПОПУЛЯЦИИ ───────────────────────────────────
		//
		// Синтетика на пять строк с ОДНИМ упоминанием имени предпосылку оси не
		// подтверждает, а СКРЫВАЕТ (`testing.md` §«Гейт на класс», п.3): живой
		// композиционный корень — две-три сотни строк, переиспользующих `err`,
		// и развилок по нему после вызова там от одной до шести.
		{"проверка на месте, НИЖЕ ещё развилки по тому же имени", 1, `func later() error { return nil }

func runServe(cfg Config) error {
	desc, err := describe(cfg)
	if err != nil {
		return err
	}
	_ = desc
	if err = later(); err != nil {
		return fmt.Errorf("later: %w", err)
	}
	if err = later(); err != nil {
		return err
	}
	return nil
}`},
		{"имя перезаписано ПОСЛЕ проверки — проверка была первой", 1, `func later() error { return nil }

func runServe(cfg Config) error {
	desc, err := describe(cfg)
	if err != nil {
		return err
	}
	_ = desc
	err = later()
	_ = err
	return nil
}`},
		{"ниже перекрытие имени во вложенной области — проверка стояла раньше", 1, `func later() error { return nil }

func runServe(cfg Config) error {
	desc, err := describe(cfg)
	if err != nil {
		return err
	}
	_ = desc
	if err := later(); err != nil {
		return err
	}
	return nil
}`},
	}
	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			reach := scanReach(t, f.caller)
			if q := reach.quenched["demo"]; len(q) != 0 {
				t.Fatalf("ЗАКОННАЯ форма %q объявлена находкой — гейт краснел бы на "+
					"исправном дереве:\n%s", f.name, strings.Join(q, "\n"))
			}
			if reach.callersSeen == 0 {
				t.Fatalf("вызовов поставщика не осмотрено — форма %q ушла в "+
					"НЕВИДИМОСТЬ, а не в молчание", f.name)
			}
			if reach.callersReach != reach.callersSeen {
				t.Fatalf("осмотрено %d, доходит %d — величины разошлись на законной "+
					"форме %q", reach.callersSeen, reach.callersReach, f.name)
			}
			// Перепись обязана назвать РОВНО столько вызовов, сколько их в
			// фикстуре. Двойной счёт одного вызова так же лжив, как пропуск:
			// на нём разрыв «осмотрено/доходит» перестал бы сходиться.
			if reach.callersSeen != f.calls {
				t.Fatalf("перепись назвала %d вызовов, а в фикстуре их %d — форма %q "+
					"считается не по разу", reach.callersSeen, f.calls, f.name)
			}
			t.Logf("законный близнец %q: осмотрено %d, доходит %d",
				f.name, reach.callersSeen, reach.callersReach)
		})
	}
}

// ТИХИЕ ПОДДЕЛКИ — по одной на форму гашения. Каждая СОБИРАЕТСЯ и каждая
// выглядит обычным кодом: громкая подделка (сломанная сборка, снятая переменная)
// доказывала бы не то — она проверяла бы компилятор, а не гейт.
func TestRefusalReachRedOnEveryQuenchingForm(t *testing.T) {
	cases := []struct {
		name   string
		caller string
		says   string
	}{
		{
			name: "гашение в `_` — форма, гасящая посадку одним символом",
			caller: `func runServe(cfg Config) error {
	desc, _ := describe(cfg)
	_ = desc
	return nil
}`,
			says: "ПОГАШЕН",
		},
		{
			name: "гашение при СОХРАНЁННОЙ проверке — она судит устаревшую переменную",
			caller: `func runServe(cfg Config) error {
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return err
	}
	_ = mode
	desc, _ := describe(cfg)
	if err != nil {
		return err
	}
	_ = desc
	return nil
}`,
			says: "ПОГАШЕН",
		},
		{
			name: "отказ принят и не проверен вовсе",
			caller: `func runServe(cfg Config) error {
	desc, derr := describe(cfg)
	_ = derr
	_ = desc
	return nil
}`,
			says: "до ОСТАНОВКИ не доводится",
		},
		{
			name: "проверяется ДРУГАЯ переменная",
			caller: `func runServe(cfg Config) error {
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return err
	}
	_ = mode
	desc, derr := describe(cfg)
	if err != nil {
		return err
	}
	_ = desc
	_ = derr
	return nil
}`,
			says: "до ОСТАНОВКИ не доводится",
		},
		{
			name: "проверка есть, выхода нет",
			caller: `func runServe(cfg Config) error {
	desc, err := describe(cfg)
	if err != nil {
		log.Printf("посадка: %v", err)
	}
	_ = desc
	return nil
}`,
			says: "до ОСТАНОВКИ не доводится",
		},
		{
			name: "вызов голым выражением — результат отброшен весь",
			caller: `func runServe(cfg Config) error {
	describe(cfg)
	return nil
}`,
			says: "голым выражением",
		},
		{
			name: "гашение УРОВНЕМ ВЫШЕ — обёртка честно передаёт, вызывающий гасит",
			caller: `func build(cfg Config) (servicecontract.Descriptor, error) {
	return describe(cfg)
}

func runServe(cfg Config) error {
	desc, _ := build(cfg)
	_ = desc
	return nil
}`,
			says: "ПОГАШЕН",
		},
		// ── ФОРМЫ ИЗ НАСТОЯЩЕЙ ПОПУЛЯЦИИ ─────────────────────────────────────
		//
		// Каждая ниже проходила ПЕРВУЮ редакцию оси насквозь: та искала любую
		// развилку по имени где угодно ниже по телу и не спрашивала, не затёрто
		// ли к тому моменту само имя. На живом дереве такая развилка есть почти
		// всегда — ответ «доходит» был тождественно истинным.
		{
			name: "проверка снята, ниже по телу есть ДРУГАЯ развилка по тому же имени",
			caller: `func later() error { return nil }

func runServe(cfg Config) error {
	desc, err := describe(cfg)
	_ = desc
	if err = later(); err != nil {
		return err
	}
	return nil
}`,
			says: "до ОСТАНОВКИ не доводится",
		},
		{
			name: "имя затёрто раньше проверки — проверка судит чужой отказ",
			caller: `func later() error { return nil }

func runServe(cfg Config) error {
	desc, err := describe(cfg)
	_ = desc
	err = later()
	if err != nil {
		return err
	}
	return nil
}`,
			says: "до ОСТАНОВКИ не доводится",
		},
		{
			name: "развилка ПЕРЕКРЫВАЕТ имя своим объявлением — это другая переменная",
			caller: `func later() error { return nil }

func runServe(cfg Config) error {
	desc, err := describe(cfg)
	_ = desc
	if err := later(); err != nil {
		return err
	}
	return nil
}`,
			says: "до ОСТАНОВКИ не доводится",
		},
		{
			name: "перекрытие во вложенной области — проверка там о нашем отказе молчит",
			caller: `func later() error { return nil }

func runServe(cfg Config) error {
	desc, err := describe(cfg)
	_ = desc
	{
		err := later()
		if err != nil {
			return err
		}
	}
	return nil
}`,
			says: "до ОСТАНОВКИ не доводится",
		},
		{
			name: "форма распознавателю не известна — находка, а не тишина",
			caller: `func use(servicecontract.Descriptor, error) {}

func runServe(cfg Config) error {
	use(describe(cfg))
	return nil
}`,
			says: "НЕ ИЗВЕСТНА",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reach := scanReach(t, c.caller)
			q := strings.Join(reach.quenched["demo"], "\n")
			if q == "" {
				t.Fatalf("тихая подделка %q НЕ найдена: страж собран, исполняется и "+
					"не может ничего остановить, а гейт этого не видит", c.name)
			}
			if !strings.Contains(q, c.says) {
				t.Fatalf("находка не называет предмет (%q) — чинить будут не то:\n%s",
					c.says, q)
			}
			if !strings.Contains(q, "services/demo/cmd/demo/d.go") {
				t.Fatalf("находка не называет КООРДИНАТУ — по ней нечего чинить:\n%s", q)
			}
			if reach.callersReach >= reach.callersSeen {
				t.Fatalf("перепись не показала разрыв: осмотрено %d, доходит %d",
					reach.callersSeen, reach.callersReach)
			}
			t.Logf("подделка найдена: осмотрено %d, доходит %d\n%s",
				reach.callersSeen, reach.callersReach, q)
		})
	}
}

// Находка обязана ДОЕХАТЬ ДО ВЕРДИКТА гейта, а не осесть в переписи. Проба
// гоняет ТУ ЖЕ ветку решения, что и гейт, — иначе доказывала бы свойство копии.
func TestRefusalReachSurfacesInTheGateVerdict(t *testing.T) {
	reach := scanReach(t, `func runServe(cfg Config) error {
	desc, _ := describe(cfg)
	_ = desc
	return nil
}`)
	findings := adjudicatePostureReach(reach, map[string]postureReachRelaxation{})
	joined := strings.Join(findings, "\n")
	if !strings.Contains(joined, "ОТКАЗ до остановки процесса НЕ ДОХОДИТ") {
		t.Fatalf("погашенный отказ не стал находкой ГЕЙТА — он осел бы в переписи, "+
			"которую никто не читает:\n%s", joined)
	}
	if !strings.Contains(joined, "demo") {
		t.Fatalf("находка не называет компонент:\n%s", joined)
	}
}

// Поставщик, которого никто не зовёт: страж собран и не исполняется. Имя
// неэкспортированное — из своего пакета вызвать его больше неоткуда, поэтому
// это НАХОДКА, а не «судить нечем».
func TestRefusalReachRedWhenTheProviderIsNeverCalled(t *testing.T) {
	reach := scanReach(t, ``)
	q := strings.Join(reach.quenched["demo"], "\n")
	if !strings.Contains(q, "не вызывается НИ РАЗУ") {
		t.Fatalf("поставщик без вызовов не стал находкой — страж собран и не "+
			"исполняется, а гейт молчит:\n%s", q)
	}
	if reach.callersSeen != 0 {
		t.Fatalf("вызовов осмотрено %d там, где их нет", reach.callersSeen)
	}
}
