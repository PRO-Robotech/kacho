// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// chart_values_keys_carry_their_own_product_prefix_injection_test.go —
// ДОКАЗАТЕЛЬСТВО СПОСОБНОСТИ СОСЕДНЕГО ГЕЙТА УПАСТЬ.
//
// Вход НАСТОЯЩИЙ: берутся объявления, прочитанные из дерева тем же сборщиком,
// которым их читает гейт. Каждый случай меняет РОВНО ОДИН факт против этого
// входа, и у каждого правила есть ЗАКОННЫЙ БЛИЗНЕЦ — форма той же записи, на
// которой гейт обязан молчать. Без близнеца красное доказывало бы лишь то, что
// проверка умеет краснеть, а не то, что она различает предмет.
//
// Контроль в обе стороны обязателен ещё и потому, что дельта здесь ВЫЧИСЛЯЕТСЯ:
// случай, изменивший больше одного факта, доказательством не является — красное
// могло прийти от соседа.

import (
	"strings"
	"testing"
)

// valuesKeyMutate — копия настоящих объявлений с ровно одной изменённой записью.
//
// Возвращает вторым значением ЧИСЛО применённых изменений: случай, не нашедший
// своей записи, обязан быть отказом, а не тихо-зелёным. Иначе переезд предмета
// превратил бы инъекцию в проверку без входа, и она осталась бы зелёной.
func valuesKeyMutate(in []chartValuesDecl, match func(chartValuesDecl) bool, to func(chartValuesDecl) chartValuesDecl) ([]chartValuesDecl, int) {
	out := make([]chartValuesDecl, len(in))
	copy(out, in)
	applied := 0
	for i, d := range out {
		if applied == 0 && match(d) {
			out[i] = to(d)
			applied++
		}
	}
	return out, applied
}

// valuesKeyDrop — копия без записей, удовлетворяющих признаку (снятие объявления корня).
func valuesKeyDrop(in []chartValuesDecl, match func(chartValuesDecl) bool) ([]chartValuesDecl, int) {
	out := make([]chartValuesDecl, 0, len(in))
	dropped := 0
	for _, d := range in {
		if match(d) {
			dropped++
			continue
		}
		out = append(out, d)
	}
	return out, dropped
}

func TestChartValuesKeysInjectionRaisesOnlyItsOwnCase(t *testing.T) {
	real := readChartValuesDecls(t)
	if len(real) == 0 {
		t.Fatal("настоящих объявлений ноль — инъекции не над чем работать, вердикт беспредметен")
	}

	// ── КОНТРОЛЬ: дерево как есть — гейт молчит ──────────────────────────────
	base, census := auditChartValuesKeys(real)
	if len(base) != 0 {
		t.Fatalf("контроль: на нетронутом дереве гейт обязан молчать, а он нашёл %d:\n%s",
			len(base), strings.Join(base, "\n"))
	}
	if census.roots == 0 || census.declared == 0 || census.mounts == 0 {
		t.Fatalf("контроль: обход пуст — корней %d, объявлений %d, путей %d",
			census.roots, census.declared, census.mounts)
	}
	t.Logf("контроль: объявлений %d · корней %d · пар сошлось %d · путей %d · находок 0",
		len(real), census.roots, census.pairOK, census.mounts)

	cases := []struct {
		name    string
		build   func() ([]chartValuesDecl, int)
		wantRed bool
		says    string // подстрока, которую находка обязана назвать
		only    string // предмет: КАЖДАЯ находка обязана назвать именно его
		mute    string // сосед, который на этом входе обязан МОЛЧАТЬ
	}{
		{
			// ПРАВИЛО A, половина «объявление снято»: ровно то, что даёт чарт,
			// который рендерится и не работает.
			name: "A/объявление корня снято — читатель осиротел",
			build: func() ([]chartValuesDecl, int) {
				return valuesKeyDrop(real, func(d chartValuesDecl) bool {
					return d.kind == "declared" && d.name == "mtls"
				})
			},
			wantRed: true, says: "`.Values.mtls`", only: "mtls", mute: "именем платформы",
		},
		{
			// ПРАВИЛО A, половина «читатель переехал»: объявление на месте,
			// читает шаблон другой корень.
			name: "A/читатель переименован — объявления нет",
			build: func() ([]chartValuesDecl, int) {
				return valuesKeyMutate(real,
					func(d chartValuesDecl) bool { return d.kind == "read" && d.name == "platform" },
					func(d chartValuesDecl) chartValuesDecl { d.name = "platformRenamed"; return d })
			},
			wantRed: true, says: "`.Values.platformRenamed`", only: "platformRenamed", mute: "именем платформы",
		},
		{
			// ЗАКОННЫЙ БЛИЗНЕЦ правила A: читатель переехал ВМЕСТЕ с объявлением.
			// Это и есть верно сделанное переименование — гейт обязан молчать.
			name: "A/близнец: читатель и объявление переехали парой",
			build: func() ([]chartValuesDecl, int) {
				moved, a := valuesKeyMutate(real,
					func(d chartValuesDecl) bool { return d.kind == "declared" && d.name == "manifests" },
					func(d chartValuesDecl) chartValuesDecl { d.name = "delivery"; return d })
				out := make([]chartValuesDecl, 0, len(moved))
				for _, d := range moved {
					if d.kind == "read" && d.name == "manifests" {
						d.name = "delivery"
					}
					out = append(out, d)
				}
				return out, a
			},
			wantRed: false,
		},
		{
			// ПРАВИЛО Б: корень назван именем платформы ПАРОЙ — читатель и
			// объявление вместе. Это ровно то состояние, в котором чарт жил до
			// переименования: пара сходится, и потому правило A молчит, а
			// оператор всё равно пишет руками чужое имя.
			name: "Б/корень назван именем платформы (читатель и объявление парой)",
			build: func() ([]chartValuesDecl, int) {
				out := make([]chartValuesDecl, 0, len(real))
				applied := 0
				for _, d := range real {
					if (d.kind == "read" || d.kind == "declared") && d.name == "platform" {
						d.name = platformValuesName
						applied++
					}
					out = append(out, d)
				}
				return out, applied
			},
			wantRed: true, says: "именем платформы",
			only: platformValuesName, mute: "НЕ ОБЪЯВЛЯЕТ",
		},
		{
			// ЗАКОННЫЙ БЛИЗНЕЦ правила Б: имя платформы ВНУТРИ слова корнем её
			// не называет. Подстрочный предикат покраснел бы здесь ложно.
			name: "Б/близнец: имя платформы подстрокой внутри слова — не находка",
			build: func() ([]chartValuesDecl, int) {
				moved, a := valuesKeyMutate(real,
					func(d chartValuesDecl) bool { return d.kind == "declared" && d.name == "kratos" },
					func(d chartValuesDecl) chartValuesDecl { d.name = platformValuesName + "stan"; return d })
				out := make([]chartValuesDecl, 0, len(moved))
				for _, d := range moved {
					if d.kind == "read" && d.name == "kratos" {
						d.name = platformValuesName + "stan"
					}
					out = append(out, d)
				}
				return out, a
			},
			wantRed: false,
		},
		{
			// ПРАВИЛО В: путь монтирования назван именем платформы.
			name: "В/путь монтирования назван именем платформы",
			build: func() ([]chartValuesDecl, int) {
				return valuesKeyMutate(real,
					func(d chartValuesDecl) bool {
						return d.kind == "mount" && strings.Contains(d.name, "kaname-identity-src")
					},
					func(d chartValuesDecl) chartValuesDecl {
						d.name = "/etc/" + platformValuesName + "-identity-src"
						return d
					})
			},
			wantRed: true, says: "монтирует путь", only: "-identity-src", mute: "НЕ ОБЪЯВЛЯЕТ",
		},
		{
			// ЗАКОННЫЙ БЛИЗНЕЦ правила В: путь без имени платформы молчит, даже
			// когда он новый. Иначе гейт судил бы «путь изменился», а не «путь
			// назван чужим именем».
			name: "В/близнец: путь со своим именем продукта — не находка",
			build: func() ([]chartValuesDecl, int) {
				return valuesKeyMutate(real,
					func(d chartValuesDecl) bool {
						return d.kind == "mount" && strings.Contains(d.name, "kaname-identity-src")
					},
					func(d chartValuesDecl) chartValuesDecl { d.name = "/etc/kaname-identity-source"; return d })
			},
			wantRed: false,
		},
		{
			// ГРАНИЦА: пространство значений ЗОНТА правилами не судится ни по
			// паре, ни по приставке. Оно и есть имя владельца.
			name: "граница: корень зонта не судится ни одним правилом",
			build: func() ([]chartValuesDecl, int) {
				return valuesKeyMutate(real,
					func(d chartValuesDecl) bool { return d.kind == "read" && d.name == umbrellaGlobalRoot },
					func(d chartValuesDecl) chartValuesDecl { return d }) // тот же факт, запись найдена
			},
			wantRed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, applied := tc.build()
			if applied == 0 {
				t.Fatalf("случай не нашёл своей записи в настоящем входе — инъекция без предмета "+
					"осталась бы зелёной и доказывала бы ноль (объявлений на входе %d)", len(real))
			}
			got, _ := auditChartValuesKeys(in)
			if tc.wantRed && len(got) == 0 {
				t.Fatalf("инъекция внесена (%d записей), а гейт молчит — он не различает этот предмет", applied)
			}
			if !tc.wantRed && len(got) != 0 {
				t.Fatalf("законный близнец обязан молчать, а гейт нашёл %d:\n%s",
					len(got), strings.Join(got, "\n"))
			}
			if tc.wantRed {
				joined := strings.Join(got, "\n")
				if !strings.Contains(joined, tc.says) {
					t.Fatalf("находка не называет предмет %q — читатель пойдёт чинить не туда:\n%s",
						tc.says, joined)
				}
				for _, f := range got {
					if !strings.Contains(f, tc.only) {
						t.Fatalf("находка не о предмете случая (%q) — красное пришло от соседа:\n%s",
							tc.only, f)
					}
				}
				if tc.mute != "" && strings.Contains(joined, tc.mute) {
					t.Fatalf("на этом входе правило %q обязано молчать — иначе случай роняет "+
						"больше проверяемого:\n%s", tc.mute, joined)
				}
			}
		})
	}
}

// Пустой вход — ОТКАЗ, а не чистое дерево: перепись обязана это показать.
func TestChartValuesKeysEmptyWalkIsNotAVerdict(t *testing.T) {
	findings, census := auditChartValuesKeys(nil)
	if len(findings) != 0 {
		t.Fatalf("на пустом входе находок быть не может, а их %d", len(findings))
	}
	if census.reads != 0 || census.declared != 0 || census.mounts != 0 {
		t.Fatalf("перепись пустого входа обязана быть нулевой: %+v", census)
	}
	// Гейт различает это по переписи (см. его ветку «обход пуст»); здесь
	// закреплено, что разбор сам по себе НОЛЬ находок на пустом входе выдаёт, —
	// то есть «чисто» и «не прочитано» неотличимы БЕЗ переписи, и потому она
	// печатается всегда.
}
