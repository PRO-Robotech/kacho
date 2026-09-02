// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package modelrender_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
	"github.com/PRO-Robotech/kacho/services/iam/internal/modelrender"
)

// canonforms_test.go — формы блока, которые НЕСЁТ действующий канон, обязаны быть
// выразимы разделом `resources` и воспроизводимы рендером.
//
// # Почему пробы идут через ЗАГРУЗЧИК, а не собирают структуру в памяти
//
// Форма считается выразимой, когда существует ВХОД МАНИФЕСТА, на котором она
// доезжает до рендера. Структура, собранная в памяти, минует разбор и его отказы,
// поэтому проба, зелёная на ней, ничего не говорит о том, примет ли загрузчик тот
// же документ: ровно так возможность и оказывается объявленной и неисполнимой
// (`api-conventions.md` §«Неисполнимая возможность»).
//
// # Что здесь утверждается — СТРОКИ, а не весь блок
//
// Блок канона обычно требует нескольких форм сразу, поэтому побайтовое равенство
// целого блока утверждается ОДИН раз — переписью достижимости (reach_test.go).
// Здесь каждая проба отвечает за СВОЮ форму и называет ту строку канона, которую
// эта форма производит; рядом стоит положительный контроль — умолчательная форма
// того же места. Без него «строка появилась» было бы неотличимо от «рендер
// печатает что угодно».

// renderFromYAML — загружает манифест и рендерит его ЕДИНСТВЕННЫЙ ресурс.
//
// Отказ загрузчика печатается целиком: он называет поле, правило и координату, и
// это ровно то, что нужно читателю красной пробы.
func renderFromYAML(t *testing.T, doc string) string {
	t.Helper()
	m, err := manifest.Load([]byte(doc))
	if err != nil {
		t.Fatalf("загрузчик отверг вход:\n%v\nвход:\n%s", err, doc)
	}
	if len(m.Resources) != 1 {
		t.Fatalf("ресурсов в манифесте %d, ожидался один", len(m.Resources))
	}
	got, err := modelrender.Render(m.Resources[0])
	if err != nil {
		t.Fatalf("рендер отказал: %v", err)
	}
	return string(got)
}

// mustContainLine требует строку ДОСЛОВНО, вместе с отступом и переводом строки.
func mustContainLine(t *testing.T, block, line string) {
	t.Helper()
	if !strings.Contains(block, line) {
		t.Fatalf("строка не порождена дословно:\n  ожидалось %q\nпорождённый блок:\n%s", line, block)
	}
}

// TestMODMR28APointerWhoseNameDiffersFromItsTypeIsExpressible — имя указателя и
// тип объекта, на который он указывает, суть РАЗНЫЕ строки (#1860).
//
// Замер, из которого проба выведена: `registry_repository` несёт
// `define parent: [registry_registry]` — единственный блок канона, у которого имя
// указателя не равно типу. Пока имя выводилось из типа, эта пара не порождалась
// НИ ПРИ КАКОМ значении ключа, то есть блок был недостижим by construction.
func TestMODMR28APointerWhoseNameDiffersFromItsTypeIsExpressible(t *testing.T) {
	block := renderFromYAML(t, `apiVersion: iam/v1
module: registry
resources:
  - name: repository
    objectType: registry_repository
    parents:
      - {name: parent, type: registry_registry}
    producer: authored
    verbs: [get]
`)
	mustContainLine(t, block, "    define parent: [registry_registry]\n")
	// Каскад берёт ИМЯ указателя, а не его тип: `super_admin from parent`.
	mustContainLine(t, block, "    define super_admin: super_admin from parent\n")
}

// TestMODMR28ThePointerWhoseNameEqualsItsTypeStaysAShortString — положительный
// контроль к пробе выше.
//
// Без него «имя и тип разделены» было бы неотличимо от «раздел принимает что
// угодно»: отрицание, не имеющее пары, зеленеет на любой сломанной форме.
func TestMODMR28ThePointerWhoseNameEqualsItsTypeStaysAShortString(t *testing.T) {
	block := renderFromYAML(t, `apiVersion: iam/v1
module: vpc
resources:
  - name: gateway
    objectType: vpc_gateway
    parents: [project]
    producer: derived
    verbs: [get, list, update, delete]
`)
	mustContainLine(t, block, "    define project: [project]\n")
	mustContainLine(t, block, "    define super_admin: super_admin from project\n")
}

// TestMODMR29ASecondScopePointerIsExpressible — указателей у блока бывает
// БОЛЬШЕ ОДНОГО (#1858).
//
// Замер по канону: `project` несёт `define cluster: [cluster]` сверх `account`,
// `iam_access_binding` — `account` и `cluster` сверх `project`. Пока указатель
// был один, эти строки не порождались ничем.
func TestMODMR29ASecondScopePointerIsExpressible(t *testing.T) {
	block := renderFromYAML(t, `apiVersion: iam/v1
module: iam
resources:
  - name: accessBinding
    objectType: iam_access_binding
    parents: [project, account, cluster]
    producer: derived
    verbs: [get, list, update, delete]
`)
	for _, line := range []string{
		"    define project: [project]\n",
		"    define account: [account]\n",
		"    define cluster: [cluster]\n",
	} {
		mustContainLine(t, block, line)
	}
	// Порядок указателей — порядок манифеста, а не сортировка: канон ставит
	// `project` первым, и перестановка дала бы другой блок.
	first := strings.Index(block, "define project:")
	second := strings.Index(block, "define account:")
	third := strings.Index(block, "define cluster:")
	if !(first < second && second < third) {
		t.Fatalf("порядок указателей не совпал с порядком манифеста:\n%s", block)
	}
}

// ── Каскад супер-доступа и источники яруса (#1858) ───────────────────────────

// TestMODMR30TheCascadeIsDeclaredByTheManifest — написаний каскада в каноне
// ЧЕТЫРЕ, и рендер знал одно.
//
// Замер по канону: `super_admin from <указатель>` — 19 блоков (это и есть
// умолчание), `any_admin from cluster` — 2, `admin from account` — 4,
// `admin from account or any_admin from cluster` — 1,
// `super_admin from project or admin from account or any_admin from cluster` — 1.
// Итого восемь блоков несут написание, которого рендер произвести не мог.
func TestMODMR30TheCascadeIsDeclaredByTheManifest(t *testing.T) {
	// Одиночный терм, отличный от умолчания: так написан vpc_address_pool.
	block := renderFromYAML(t, `apiVersion: iam/v1
module: vpc
resources:
  - name: addressPool
    objectType: vpc_address_pool
    parents: [cluster]
    producer: authored
    cascade:
      - {relation: any_admin, from: cluster}
    verbs: [get]
`)
	mustContainLine(t, block, "    define super_admin: any_admin from cluster\n")

	// Дизъюнкция термов: так написан iam_access_binding.
	block = renderFromYAML(t, `apiVersion: iam/v1
module: iam
resources:
  - name: accessBinding
    objectType: iam_access_binding
    parents: [project, account, cluster]
    producer: derived
    cascade:
      - {relation: super_admin, from: project}
      - {relation: admin, from: account}
      - {relation: any_admin, from: cluster}
    verbs: [get]
`)
	mustContainLine(t, block,
		"    define super_admin: super_admin from project or admin from account or any_admin from cluster\n")
}

// TestMODMR30TheDefaultCascadeStaysUnwritten — положительный контроль: 19 блоков
// канона из 27 несут умолчание, и оно остаётся неписаным.
func TestMODMR30TheDefaultCascadeStaysUnwritten(t *testing.T) {
	block := renderFromYAML(t, `apiVersion: iam/v1
module: vpc
resources:
  - name: gateway
    objectType: vpc_gateway
    parents: [project]
    producer: derived
    verbs: [get, list, update, delete]
`)
	mustContainLine(t, block, "    define super_admin: super_admin from project\n")
}

// TestMODMR31ATierMayDeriveFromMoreThanTheChain — ярус выводится не только от
// предыдущего.
//
// Замер: `account` несёт `define admin: [...] or owner or super_admin`,
// `iam_user` — `define viewer: [...] or subject or editor`. Цепочка ярусов у
// рендера была одна, и оба написания она не давала ни при каком входе.
func TestMODMR31ATierMayDeriveFromMoreThanTheChain(t *testing.T) {
	block := renderFromYAML(t, `apiVersion: iam/v1
module: iam
resources:
  - name: account
    objectType: account
    parents: [cluster]
    producer: derived
    cascade:
      - {relation: any_admin, from: cluster}
    relations:
      - {name: owner, definition: "[user]"}
    tiers:
      - {name: admin, from: [owner, super_admin]}
      - editor
      - viewer
    verbs: [get]
`)
	mustContainLine(t, block,
		"    define admin: [user, service_account, group#member] or owner or super_admin\n")
	// Цепочка после нестандартного яруса продолжается от него же.
	mustContainLine(t, block,
		"    define editor: [user, service_account, group#member] or admin\n")
}

// TestMODMR31TheDefaultTierChainStaysAShortString — положительный контроль:
// ярус, выводимый от предыдущего, пишется именем и ничем больше.
func TestMODMR31TheDefaultTierChainStaysAShortString(t *testing.T) {
	block := renderFromYAML(t, `apiVersion: iam/v1
module: vpc
resources:
  - name: addressPool
    objectType: vpc_address_pool
    parents: [cluster]
    producer: authored
    cascade:
      - {relation: any_admin, from: cluster}
    subjects: [user, service_account]
    tiers: [admin, viewer]
    verbs: [get]
`)
	mustContainLine(t, block, "    define admin: [user, service_account] or super_admin\n")
	mustContainLine(t, block, "    define viewer: [user, service_account] or admin\n")
}
