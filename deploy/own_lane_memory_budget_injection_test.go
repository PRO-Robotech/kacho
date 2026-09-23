// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_memory_budget_injection_test.go — доказательство того, что сверка
// бюджета полосы СПОСОБНА упасть и способна смолчать.
//
// Вход СИНТЕТИЧЕСКИЙ: подделка дерева трогает общий клон, а вердикт обязан
// доказываться на входе, построенном здесь и целиком видном читателю.
//
// Каждый случай меняет РОВНО ОДИН факт против законного близнеца. Величина
// памяти одной проверки подаётся аргументом, поэтому оси не зависят ни от пина,
// ни от кэша модулей: предмет здесь — АРИФМЕТИКА и ветвление, а не потолок.
package deploy_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// injPerCheck — память одной проверки в осях ниже. Круглое число выбрано
// намеренно: арифметику отказа должно быть видно глазом.
const injPerCheck uint64 = 100

// legalBudgetStack — законный близнец: предел ровно покрывает бюджет.
// 8 × 100 + 200 = 1000.
func legalBudgetStack() laneBudgetFacts {
	return laneBudgetFacts{
		Stack: "стенд", Posture: "external",
		Capacity: 8, Reserve: 200, Limit: 1000,
		Declared: true, HasLimit: true,
	}
}

func TestLaneMemoryBudgetJudgement_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(f *laneBudgetFacts)
		want    int
		mustSay string
	}{
		{
			name:   "законный близнец: предел РОВНО покрывает бюджет — молчит",
			mutate: func(*laneBudgetFacts) {},
			want:   0,
		},
		{
			name:    "предел на байт меньше бюджета — находка",
			mutate:  func(f *laneBudgetFacts) { f.Limit = 999 },
			want:    1,
			mustSay: "ПРЕВЫШАЕТ предел памяти контейнера",
		},
		{
			name:    "предел не объявлен, числа полосы объявлены — находка",
			mutate:  func(f *laneBudgetFacts) { f.HasLimit = false },
			want:    1,
			mustSay: "resources.limits.memory",
		},
		{
			name:   "предел ВЫШЕ бюджета — НЕ находка: сверка судит достаточность, а не равенство",
			mutate: func(f *laneBudgetFacts) { f.Limit = 4096 },
			want:   0,
		},
		{
			// ЗАКОННЫЙ БЛИЗНЕЦ: стенд, не объявивший чисел полосы и стоящий не на
			// `own`, предметом не является — требовать от него предела значило бы
			// краснеть на исправном дереве.
			name: "ни чисел полосы, ни посадки own — НЕ находка",
			mutate: func(f *laneBudgetFacts) {
				f.Declared, f.HasLimit = false, false
			},
			want: 0,
		},
		{
			name: "посадка own БЕЗ чисел полосы — находка: стражу нечем сверять",
			mutate: func(f *laneBudgetFacts) {
				f.Posture, f.Declared = "own", false
			},
			want:    1,
			mustSay: "ёмкость и резерв полосы не объявлены",
		},
		{
			name: "посадка own без предела — находка, хотя на external тот же стенд молчал бы",
			mutate: func(f *laneBudgetFacts) {
				f.Posture, f.HasLimit = "own", false
			},
			want:    1,
			mustSay: "предел памяти средой не наложен",
		},
		{
			name:    "непозитивная ёмкость — находка ДО арифметики",
			mutate:  func(f *laneBudgetFacts) { f.Capacity = 0 },
			want:    1,
			mustSay: "непозитивной",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := legalBudgetStack()
			c.mutate(&f)
			findings, census := judgeLaneMemoryBudget([]laneBudgetFacts{f}, injPerCheck)
			if len(findings) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(findings), c.want, findings)
			}
			if c.mustSay != "" {
				var said bool
				for _, got := range findings {
					if strings.Contains(got, c.mustSay) {
						said = true
					}
				}
				if !said {
					t.Errorf("ни одна находка не называет %q — оператор не узнает причину: %v",
						c.mustSay, findings)
				}
			}
			if census.Stacks != 1 {
				t.Errorf("перепись осмотренного %d, подан 1 — «находок ноль» стало бы "+
					"неотличимо от «прочитано ноль»", census.Stacks)
			}
		})
	}
}

// TestLaneMemoryBudgetJudgement_NamesTheNeededNumber — отказ обязан называть
// величину, до которой поднимать предел. Отказ без неё восстанавливает не
// следующий шаг, а лишь факт неудачи.
func TestLaneMemoryBudgetJudgement_NamesTheNeededNumber(t *testing.T) {
	f := legalBudgetStack()
	f.Limit = 1
	findings, _ := judgeLaneMemoryBudget([]laneBudgetFacts{f}, injPerCheck)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1", len(findings))
	}
	if !strings.Contains(findings[0], "Поднимите предел до 1000 байт") {
		t.Errorf("отказ не называет требуемой величины: %s", findings[0])
	}
}

// lawfulFormatTable — законный близнец таблицы форматов пина: та же форма, что у
// пиненного `internal/domain/password_hash_format.go`, урезанная до того, что
// читает проба. Каждый случай ниже меняет в нём РОВНО ОДНО место.
const lawfulFormatTable = `package domain

const (
	CostParamBcryptCost   PasswordHashCostParam = "cost"
	CostParamArgon2Memory PasswordHashCostParam = "memory"
)

const bcryptMemoryPerVerificationBytes uint64 = 10328

var passwordHashFormats = []PasswordHashFormatRecord{
	{
		Format:   PasswordHashFormatBcrypt,
		Writable: false,
		Ceiling: map[PasswordHashCostParam]uint32{
			CostParamBcryptCost: 14,
		},
	},
	{
		Format:   PasswordHashFormatArgon2id,
		Writable: true,
		Ceiling: map[PasswordHashCostParam]uint32{
			CostParamArgon2Memory: 131072,
		},
		Floor: map[PasswordHashCostParam]uint32{
			CostParamArgon2Memory: 65536,
		},
	},
}

func (r PasswordHashFormatRecord) MemoryPerVerificationAtCeilingBytes() uint64 {
	if memory, ok := r.Ceiling[CostParamArgon2Memory]; ok {
		return uint64(memory) * 1024
	}
	return bcryptMemoryPerVerificationBytes
}
`

// argon2CeilingBytes — память одной проверки на потолке законного близнеца:
// 131072 КиБ × 1024.
const argon2CeilingBytes uint64 = 131072 * 1024

// TestLaneBudgetCeilingReader_UnreadIsNotAbsent — читатель источника истины
// различает «законно нет» и «не прочитано» (kacho#2726): у записи без потолка
// памяти argon2 страж берёт постоянную bcrypt — это законный исход; запись, чью
// форму читатель не знает, — отказ с причиной, а не тот же исход молча.
func TestLaneBudgetCeilingReader_UnreadIsNotAbsent(t *testing.T) {
	cases := []struct {
		name         string
		from, to     string // одна подмена в законном близнеце; пусто — без подмены
		wantPerCheck uint64 // при законном исходе
		wantErr      string // при отказе — что он обязан назвать
	}{
		{
			name:         "законный близнец: потолок десятичным литералом — прочитан",
			wantPerCheck: argon2CeilingBytes,
		},
		{
			name:    "потолок выражением той же величины — не прочитан, отказ",
			from:    "CostParamArgon2Memory: 131072,",
			to:      "CostParamArgon2Memory: 128 * 1024,",
			wantErr: "не прочитан",
		},
		{
			name:    "потолок сдвигом — не прочитан, отказ",
			from:    "CostParamArgon2Memory: 131072,",
			to:      "CostParamArgon2Memory: 1 << 17,",
			wantErr: "не прочитан",
		},
		{
			name:    "потолок именем постоянной — не прочитан, отказ",
			from:    "CostParamArgon2Memory: 131072,",
			to:      "CostParamArgon2Memory: argon2MemoryCeilingKiB,",
			wantErr: "не прочитан",
		},
		{
			name:         "потолок шестнадцатеричным литералом — законная форма Go, прочитан",
			from:         "CostParamArgon2Memory: 131072,",
			to:           "CostParamArgon2Memory: 0x20000,",
			wantPerCheck: argon2CeilingBytes,
		},
		{
			name:         "потолок литералом с разделителями — законная форма Go, прочитан",
			from:         "CostParamArgon2Memory: 131072,",
			to:           "CostParamArgon2Memory: 131_072,",
			wantPerCheck: argon2CeilingBytes,
		},
		{
			name:         "пол выражением — пол не читается вовсе, потолок прочитан",
			from:         "CostParamArgon2Memory: 65536,",
			to:           "CostParamArgon2Memory: 64 * 1024,",
			wantPerCheck: argon2CeilingBytes,
		},
		{
			name:    "карта потолка не литералом, а переменной — не прочитан, отказ",
			from:    "Ceiling: map[PasswordHashCostParam]uint32{\n\t\t\tCostParamArgon2Memory: 131072,\n\t\t},",
			to:      "Ceiling: argon2Ceiling,",
			wantErr: "не прочитан",
		},
		{
			name:    "ключ карты потолка не именем — не прочитан, отказ",
			from:    "CostParamArgon2Memory: 131072,",
			to:      "PasswordHashCostParam(\"memory\"): 131072,",
			wantErr: "не прочитан",
		},
		{
			name:    "у записи нет поля потолка — не прочитан, отказ",
			from:    "Ceiling: map[PasswordHashCostParam]uint32{\n\t\t\tCostParamArgon2Memory: 131072,\n\t\t},\n",
			to:      "",
			wantErr: "не прочитан",
		},
		{
			name:    "постоянная bcrypt выражением при записи без памяти argon2 — не прочитана, отказ",
			from:    "bcryptMemoryPerVerificationBytes uint64 = 10328",
			to:      "bcryptMemoryPerVerificationBytes uint64 = 10 * 1033",
			wantErr: "bcryptMemoryPerVerificationBytes",
		},
		{
			name:    "правило стража читает потолок другим ключом — модель пробы устарела, отказ",
			from:    "r.Ceiling[CostParamArgon2Memory]",
			to:      "r.Ceiling[CostParamArgon2MemoryKiB]",
			wantErr: "правило стража",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := lawfulFormatTable
			if c.from != "" {
				if n := strings.Count(src, c.from); n != 1 {
					t.Fatalf("фикстура: подменяемое место встречается %d раз, ожидалось 1 — случай "+
						"не менял бы ровно один факт: %q", n, c.from)
				}
				src = strings.Replace(src, c.from, c.to, 1)
			}
			file, err := parser.ParseFile(token.NewFileSet(), "password_hash_format.go", src, 0)
			if err != nil {
				t.Fatalf("фикстура не разбирается как Go: %v", err)
			}
			reading, err := readFormatTable(file)
			got := reading.PerCheck
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("читатель вернул %d байт и НЕ отказал — непрочитанное прошло за законное "+
						"отсутствие, и бюджет упал бы до постоянной bcrypt молча", got)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Errorf("отказ не называет %q: %v", c.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("законная форма отвергнута: %v", err)
			}
			if got != c.wantPerCheck {
				t.Errorf("прочитано %d байт на проверку, ожидалось %d", got, c.wantPerCheck)
			}
			// Перепись: обе записи прочитаны, и исход каждой назван — «законно
			// нет» у bcrypt посчитан отдельно от прочитанного потолка argon2.
			if reading.Records != 2 || reading.Argon2 != 1 || reading.Bcrypt != 1 {
				t.Errorf("перепись %s, ожидалось записей 2 · argon2 1 · bcrypt 1", reading)
			}
		})
	}
}

// TestLaneBudgetCeilingReader_FallbackWouldHideTheDefect — почему «не прочитано»
// обязано отказывать, а не падать на постоянную bcrypt: на ней предел 512Mi —
// ровно тот дефект, ради которого проба заведена, — прошёл бы зелёным.
func TestLaneBudgetCeilingReader_FallbackWouldHideTheDefect(t *testing.T) {
	f := laneBudgetFacts{
		Stack: "стенд", Posture: "own",
		Capacity: 8, Reserve: 268435456, Limit: 512 << 20,
		Declared: true, HasLimit: true,
	}
	if hidden, _ := judgeLaneMemoryBudget([]laneBudgetFacts{f}, 10328); len(hidden) != 0 {
		t.Fatalf("на постоянной bcrypt предел 512Mi дал находки %v — довод этой пробы неверен", hidden)
	}
	if found, _ := judgeLaneMemoryBudget([]laneBudgetFacts{f}, argon2CeilingBytes); len(found) != 1 {
		t.Fatalf("на потолке argon2 предел 512Mi дал %d находок, ожидалась 1", len(found))
	}
}

// TestLaneBudgetDeclaredNumbers_UnreadIsNotAValue — числа бюджета из профиля:
// значение, которое не есть то число, что объявлено, — отказ, а не прижатая
// или усечённая величина (тот же класс, что у читателя таблицы форматов,
// kacho#2726). Усечённая ёмкость и прижатый к нулю резерв дали бы бюджет
// МЕНЬШЕ объявленного — «не прочитано» под видом числа.
func TestLaneBudgetDeclaredNumbers_UnreadIsNotAValue(t *testing.T) {
	ints := []struct {
		name    string
		v       any
		want    int64
		wantErr bool
	}{
		{name: "законный близнец: целое YAML", v: 8, want: 8},
		{name: "законный близнец: целое, пришедшее числом с плавающей точкой", v: 8.0, want: 8},
		{name: "законный близнец: целое строкой", v: " 8 ", want: 8},
		{name: "дробное — не то число, что объявлено: отказ", v: 8.5, wantErr: true},
		{name: "не число строкой — отказ", v: "восемь", wantErr: true},
	}
	for _, c := range ints {
		t.Run("целое/"+c.name, func(t *testing.T) {
			got, err := declaredInt(c.v)
			if c.wantErr {
				if err == nil {
					t.Fatalf("%v прочитано как %d без отказа", c.v, got)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("%v: прочитано %d (%v), ожидалось %d", c.v, got, err, c.want)
			}
		})
	}
	reserves := []struct {
		name    string
		v       any
		want    uint64
		wantErr bool
	}{
		{name: "законный близнец: положительный резерв", v: 268435456, want: 268435456},
		{name: "отрицательный резерв — байтами не читается: отказ", v: -1, wantErr: true},
	}
	for _, c := range reserves {
		t.Run("резерв/"+c.name, func(t *testing.T) {
			got, err := declaredReserveBytes(c.v)
			if c.wantErr {
				if err == nil {
					t.Fatalf("%v прочитано как %d байт без отказа", c.v, got)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("%v: прочитано %d (%v), ожидалось %d", c.v, got, err, c.want)
			}
		})
	}
}
