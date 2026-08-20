// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// config_admission_test.go — ручки потолка допуска доезжают ИЗ ОКРУЖЕНИЯ.
//
// # Почему без этой пробы опечатка в теге молчит
//
// Незаданная ручка означает ПОЛ ПЛАТФОРМЫ, и это правильно. Цена правильного
// умолчания — что опечатка в имени группы (`ADMISSON_PUBLIC`) выглядит В ТОЧНОСТИ
// как «посадка не переопределяла»: библиотека незнакомую переменную просто не
// читает, поле остаётся нулём, пол подставляется, процесс поднимается и пишет в
// журнал взведённый потолок. То есть ручка объявлена, задокументирована — и
// задать её нельзя ни при каком вводе.
//
// Поэтому проба утверждает ДОЕЗД, а не наличие поля, и утверждает его на ОБЕИХ
// группах: две группы одной структуры — классическое место, где вторая читает
// переменные первой.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

// TestAdmissionKnobsArriveFromThePosture — обе группы читаются, и читаются
// НЕЗАВИСИМО.
func TestAdmissionKnobsArriveFromThePosture(t *testing.T) {
	admissionRequiredEnv(t)
	t.Setenv("KACHO_COMPUTE_ADMISSION_PUBLIC_READ_PER_SEC", "7")
	t.Setenv("KACHO_COMPUTE_ADMISSION_PUBLIC_MUTATION_PER_SEC", "3")
	t.Setenv("KACHO_COMPUTE_ADMISSION_PUBLIC_BURST_FACTOR", "2")
	t.Setenv("KACHO_COMPUTE_ADMISSION_PUBLIC_IN_FLIGHT", "5")
	t.Setenv("KACHO_COMPUTE_ADMISSION_INTERNAL_READ_PER_SEC", "70")
	t.Setenv("KACHO_COMPUTE_ADMISSION_INTERNAL_MUTATION_PER_SEC", "30")
	t.Setenv("KACHO_COMPUTE_ADMISSION_INTERNAL_BURST_FACTOR", "4")
	t.Setenv("KACHO_COMPUTE_ADMISSION_INTERNAL_IN_FLIGHT", "50")

	c, err := config.Load()
	if err != nil {
		t.Fatalf("настройки не загрузились: %v", err)
	}
	if got := c.AdmissionPublic; got.ReadPerSec != 7 || got.MutationPerSec != 3 ||
		got.BurstFactor != 2 || got.InFlight != 5 {
		t.Fatalf("группа публичного слушателя не доехала из окружения: %+v\n\n"+
			"Опечатка в теге группы неотличима от «посадка молчит»: поле остаётся нулём, "+
			"подставляется пол платформы, и ручка не работает ни при каком вводе", got)
	}
	if got := c.AdmissionInternal; got.ReadPerSec != 70 || got.MutationPerSec != 30 ||
		got.BurstFactor != 4 || got.InFlight != 50 {
		t.Fatalf("группа внутреннего слушателя не доехала из окружения: %+v", got)
	}
	// Группы обязаны быть РАЗНЫМИ переменными: одна структура под двумя полями —
	// то место, где вторая молча читает переменные первой.
	if c.AdmissionPublic == c.AdmissionInternal {
		t.Fatalf("обе группы прочитали одно и то же (%+v) — значит имена переменных совпали, "+
			"и посадка не может задать слушателям разные величины", c.AdmissionPublic)
	}
}

// TestAdmissionKnobsAreSilentWhenThePostureSaysNothing — законный близнец:
// незаданные ручки МОЛЧАТ, то есть носитель подставит пол платформы.
//
// Без него проба выше зеленела бы и на структуре, чьи поля кто-то заполняет
// умолчанием тега, — а умолчание в теге здесь запрещено: полы у слушателей
// разные, и умолчание пришлось бы написать второй прописью чисел фундамента.
func TestAdmissionKnobsAreSilentWhenThePostureSaysNothing(t *testing.T) {
	admissionRequiredEnv(t)
	c, err := config.Load()
	if err != nil {
		t.Fatalf("настройки не загрузились: %v", err)
	}
	if !c.AdmissionPublic.IsSilent() || !c.AdmissionInternal.IsSilent() {
		t.Fatalf("незаданные ручки не молчат (public=%+v internal=%+v): значит у них появилось "+
			"умолчание, и «посадка назвала свои величины» стало неотличимо от «посадка молчит»",
			c.AdmissionPublic, c.AdmissionInternal)
	}
}

// admissionRequiredEnv задаёт обязательные переменные, без которых загрузчик
// отказывает раньше, чем доходит до предмета пробы. Их значения здесь
// БЕССМЫСЛЕННЫ намеренно: правдоподобное значение прячет дефект, который само же
// и кормит, а предмет пробы — имена ручек потолка, а не работоспособность БД.
func admissionRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("KACHO_COMPUTE_DB_PASSWORD", "не-пароль-а-заглушка")
}
