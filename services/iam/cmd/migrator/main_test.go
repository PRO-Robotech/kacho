// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// main_test.go — разбор командной строки мигратора kacho-iam. БД не
// открывается: пробы быстрые и не зависят от docker. Настоящий накат — предмет
// integration-проб репозитория.
package main

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func emptyFS() fs.FS { return fstest.MapFS{} }

// runCommand — разбирает args деревом команд и возвращает исход cobra.
func runCommand(t *testing.T, args []string, env map[string]string) (stdout, stderr string, err error) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	cmd := newRootCmd(emptyFS())
	var sout, serr bytes.Buffer
	cmd.SetOut(&sout)
	cmd.SetErr(&serr)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return sout.String(), serr.String(), err
}

// TestExtraPositionalArgumentIsRefused — задача #1461: лишний позиционный
// аргумент обязан получить ответ, а не молчаливый накат.
//
// Cobra по умолчанию (`Args == nil`) принимает произвольные позиционные
// аргументы. Прогон настоящего дерева команд показал это дословно: `up 800001`
// — догадка оператора о том, как задать цель — проходил разбор молча и уезжал
// накатывать ДО ГОЛОВЫ на базе из конфигурации. Прямая четвёрка теперь такой
// аргумент называет; делегирующая тройка обязана отвечать так же, иначе
// «один результат у всех семи» не выполняется.
//
// `--dialect bogus` в аргументах — не украшение: он делает пробу быстрой и
// разом утверждает ПОРЯДОК. До правки отказ приходил от диалекта (или, без
// него, от двухминутного барьера готовности БД); после — от разбора
// аргументов, который стоит РАНЬШЕ любого исполнения.
func TestExtraPositionalArgumentIsRefused(t *testing.T) {
	for _, sub := range []string{"up", "down", "status"} {
		_, _, err := runCommand(t, []string{"--dialect", "bogus", sub, "800001"}, nil)
		if err == nil {
			t.Fatalf("%s 800001: лишний аргумент принят молча", sub)
		}
		if !strings.Contains(err.Error(), "800001") {
			t.Errorf("%s 800001: отказ не называет лишний аргумент: %v", sub, err)
		}
	}
}

// TestLegitimateFlagsSurviveTheArgumentCheck — положительный контроль к пробе
// выше. Без него она зеленела бы на дереве команд, отвергающем вообще всё:
// законный вызов обязан дойти до исполнения, и отказ прийти от диалекта.
func TestLegitimateFlagsSurviveTheArgumentCheck(t *testing.T) {
	for _, args := range [][]string{
		{"--dialect", "bogus", "up"},
		{"up", "--dialect", "bogus"},
		{"up", "--dialect", "bogus", "--target", "800001"},
	} {
		_, _, err := runCommand(t, args, nil)
		if err == nil {
			t.Fatalf("%v: ожидался отказ по диалекту, получено nil", args)
		}
		if strings.Contains(err.Error(), "800001") && !strings.Contains(err.Error(), "dialect") {
			t.Errorf("%v: законный вызов отвергнут разбором аргументов: %v", args, err)
		}
		if !strings.Contains(err.Error(), "dialect") {
			t.Errorf("%v: отказ пришёл не от диалекта, значит проба утверждает не то: %v", args, err)
		}
	}
}
