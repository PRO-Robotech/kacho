// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// stand_cloud_admin_bootstrap_render_test.go — АДРЕС ПЕРВОГО АДМИНИСТРАТОРА
// ОБЛАКА ДОЕЗЖАЕТ ДО СЛУЖБЫ ДОСТУПА ССЫЛКОЙ НА СЕКРЕТ, А НЕ ВЕЛИЧИНОЙ (kacho#2878).
//
// Служба доступа выдаёт право администратора облака человеку с адресом из
// KANAME_BOOTSTRAP_ROOT_EMAIL (посев бутстрапа). Профиль стенда называет ИМЯ
// секрета (`kaname.platform.iam.bootstrapRootAdmin.secretName`), и рендер
// обязан отдать переменную ссылкой `secretKeyRef` на этот секрет —
// обязательной: стенд без секрета не поднимается молча без администратора.
// Близнец: профиль, секрета не объявивший, переменной не несёт. Инъекция:
// переменная, объявленная и ссылкой, и литералом карты `env`, — отказ рендера:
// два места об одном предмете.
package deploy_test

import (
	"strings"
	"testing"
)

const bootstrapRootEnv = "KANAME_BOOTSTRAP_ROOT_EMAIL"

// kanameEnvOf — запись переменной в главном контейнере рабочего объекта службы.
func kanameEnvOf(dep renderedDoc, name string) (map[string]any, int) {
	cs, _ := podSpec(dep)["containers"].([]any)
	if len(cs) == 0 {
		return nil, 0
	}
	c, _ := cs[0].(map[string]any)
	env, _ := c["env"].([]any)
	var hit map[string]any
	n := 0
	for _, e := range env {
		m, _ := e.(map[string]any)
		if m["name"] == name {
			hit = m
			n++
		}
	}
	return hit, n
}

func TestBootstrapRootEmailReachesTheServiceAsASecretReference(t *testing.T) {
	own := renderNamedStack(t, cloudAdminStack)
	_, dep := own.serviceConfig(t)
	v, _ := lookup(own.values, strings.Split(cloudAdminSecretPath, ".")...)
	want := yamlScalarOf(v)
	e, n := kanameEnvOf(dep, bootstrapRootEnv)
	t.Logf("стек %s: объявлен секрет %q; записей %s в контейнере службы: %d", own.name, want, bootstrapRootEnv, n)
	if n != 1 {
		t.Fatalf("стек %s: записей %s %d, ждали ровно одну", own.name, bootstrapRootEnv, n)
	}
	if _, literal := e["value"]; literal {
		t.Errorf("стек %s: %s отдана литералом — адрес оказался бы в карте значений", own.name, bootstrapRootEnv)
	}
	ref, _ := lookup(e, "valueFrom", "secretKeyRef")
	r, _ := ref.(map[string]any)
	if r == nil {
		t.Fatalf("стек %s: %s не ссылается на секрет (%v)", own.name, bootstrapRootEnv, e)
	}
	if r["name"] != want || r["key"] != "email" {
		t.Errorf("стек %s: ссылка %v, ждали секрет %q ключ email", own.name, r, want)
	}
	if opt, _ := r["optional"].(bool); opt {
		t.Errorf("стек %s: ссылка необязательна — без секрета стенд поднялся бы молча без администратора", own.name)
	}

	prod := renderNamedStack(t, addrGateProdStack)
	_, pdep := prod.serviceConfig(t)
	if _, pn := kanameEnvOf(pdep, bootstrapRootEnv); pn != 0 {
		t.Errorf("близнец: стек %s секрета не объявляет, а %s в контейнере службы есть (%d)", prod.name, bootstrapRootEnv, pn)
	}
}

func TestBootstrapRootEmailDeclaredTwiceIsRefusedByRender(t *testing.T) {
	chain := deployStacks(t)[cloudAdminStack]
	if _, err := renderStack(t, chain); err != nil {
		t.Fatalf("законный близнец: стек %s отвергнут рендером: %v", cloudAdminStack, err)
	}
	out, err := renderStack(t, chain, "kaname.env."+bootstrapRootEnv+"=someone")
	if err == nil {
		t.Fatalf("инъекция: %s объявлена и ссылкой, и литералом карты env — рендер прошёл", bootstrapRootEnv)
	}
	if !strings.Contains(out, bootstrapRootEnv) {
		t.Errorf("инъекция: рендер отказал, но текст не называет %s:\n%s", bootstrapRootEnv, lastLines(out, 5))
	}
}
