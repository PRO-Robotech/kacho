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

// ЗАГЛУШКА, КОТОРАЯ СОБИРАЕТСЯ. Дескриптор принят, вызов на месте, импорты целы,
// компилятор доволен — и отказ выброшен в `_`. Это и есть форма, которую поиск
// подстроки не отличает от исправной: `servicecontract.New(servicecontract.Spec{`
// встречается в обоих случаях дословно.
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

// ── ЗАГЛУШКА, КОТОРАЯ СОБИРАЕТСЯ: отказ дескриптора выброшен в `_` ─────────

func TestPostureReachGateRedWhenTheDescriptorRefusalIsDiscarded(t *testing.T) {
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

func synthWitnessDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", name, err)
		}
	}
	return dir
}

// ── направление (а): свидетеля НЕТ, хотя файл выглядит как свидетельство ───

func TestRefusalWitnessGateRedOnAFileLevelDecoy(t *testing.T) {
	dir := synthWitnessDir(t, map[string]string{"decoy_test.go": injWitnessDecoySrc})
	w, err := scanContractRefusalWitness(dir)
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
	dir := synthWitnessDir(t, map[string]string{"w_test.go": injWitnessWrongErrSrc})
	w, err := scanContractRefusalWitness(dir)
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
			dir := synthWitnessDir(t, map[string]string{"w_test.go": f.src})
			w, err := scanContractRefusalWitness(dir)
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
	dir := synthWitnessDir(t, map[string]string{"w_test.go": injWitnessDelegatedSrc})
	w, err := scanContractRefusalWitness(dir)
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
	dir := synthWitnessDir(t, map[string]string{"w_test.go": acceptOnly})
	w, err := scanContractRefusalWitness(dir)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(w.direct) != 0 {
		t.Fatalf("положительный контроль засчитан за свидетеля отказа: %v", w.direct)
	}
}

// ПУСТОЙ ПАКЕТ — перепись обязана это показать, а гейт выше — отказать.
func TestRefusalWitnessRefusesAnEmptyPackage(t *testing.T) {
	dir := synthWitnessDir(t, map[string]string{"README.md": "проб нет\n"})
	w, err := scanContractRefusalWitness(dir)
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
