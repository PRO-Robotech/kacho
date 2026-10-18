// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// subscription_stream_ceiling_test.go — kacho#3214: потолки потоков подписки
// называются ОДНОЙ величиной в трёх местах — умолчанием процесса, профилем
// чарта и страницей конфигурации, — и процесс без ручек стартует с величиной,
// которую принимает владелец журнала.
//
// Прежде процесс без ручек брал 64 и 8, страница называла 64 и 8, а профиль —
// 16 и 12, и профиль же объяснял, что 64 вчетверо больше того, что принимает
// владелец: при умолчаниях процесса исчерпание наступало у владельца, то есть у
// всех арендаторов сразу.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

const (
	knobMaxStreams           = "KACHO_API_GATEWAY_SUBSCRIPTION_MAX_STREAMS"
	knobMaxStreamsPerSubject = "KACHO_API_GATEWAY_SUBSCRIPTION_MAX_STREAMS_PER_SUBJECT"
)

// processDefaults — потолки процесса, собранного загрузчиком без обеих ручек.
func processDefaults(t *testing.T) (perReplica, perSubject int) {
	t.Helper()
	for _, k := range []string{knobMaxStreams, knobMaxStreamsPerSubject} {
		if v, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { _ = os.Setenv(k, v) })
			_ = os.Unsetenv(k)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load без ручек потолков: %v", err)
	}
	return cfg.SubscriptionMaxStreams, cfg.SubscriptionMaxStreamsPerSubject
}

// chartCeilings — `subscriptionStream.maxStreams` / `maxStreamsPerSubject`
// профиля чарта края.
func chartCeilings(t *testing.T) (perReplica, perSubject int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		SubscriptionStream struct {
			MaxStreams           int `yaml:"maxStreams"`
			MaxStreamsPerSubject int `yaml:"maxStreamsPerSubject"`
		} `yaml:"subscriptionStream"`
	}
	if err := yaml.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.SubscriptionStream.MaxStreams == 0 || v.SubscriptionStream.MaxStreamsPerSubject == 0 {
		t.Fatal("профиль чарта не объявляет потолков — сверять не с чем")
	}
	return v.SubscriptionStream.MaxStreams, v.SubscriptionStream.MaxStreamsPerSubject
}

// ownerCeilings — `subscription.maxStreams` чарта каждого владельца журнала в
// дереве (services/*/deploy/values.yaml).
func ownerCeilings(t *testing.T) map[string]int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "services", "*", "deploy", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, f := range files {
		raw, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		var v struct {
			Subscription struct {
				MaxStreams int `yaml:"maxStreams"`
			} `yaml:"subscription"`
		}
		if yaml.Unmarshal(raw, &v) == nil && v.Subscription.MaxStreams > 0 {
			out[f] = v.Subscription.MaxStreams
		}
	}
	return out
}

// pageDefault — колонка «По умолчанию» строки ручки на странице конфигурации.
func pageDefault(t *testing.T, page, knob string) int {
	t.Helper()
	escaped := regexp.MustCompile(`_`).ReplaceAllString(knob, "&#95;")
	re := regexp.MustCompile(`<tr><td><code>` + regexp.QuoteMeta(escaped) + `</code></td><td><code>(\d+)</code></td>`)
	m := re.FindAllStringSubmatch(page, -1)
	if len(m) != 1 {
		t.Fatalf("строк ручки %s с числовым умолчанием на странице %d, ожидалась одна", knob, len(m))
	}
	n, _ := strconv.Atoi(m[0][1])
	return n
}

func TestStreamCeilingsAreOneValueInProcessChartAndPage(t *testing.T) {
	procReplica, procSubject := processDefaults(t)
	chartReplica, chartSubject := chartCeilings(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "content", "install", "configuration.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	pageReplica := pageDefault(t, page, knobMaxStreams)
	pageSubject := pageDefault(t, page, knobMaxStreamsPerSubject)

	t.Logf("потолок реплики: процесс %d · профиль %d · страница %d", procReplica, chartReplica, pageReplica)
	t.Logf("потолок вызывающего: процесс %d · профиль %d · страница %d", procSubject, chartSubject, pageSubject)
	if procReplica != chartReplica || pageReplica != chartReplica {
		t.Errorf("потолок реплики назван разными величинами: процесс %d, профиль %d, страница %d",
			procReplica, chartReplica, pageReplica)
	}
	if procSubject != chartSubject || pageSubject != chartSubject {
		t.Errorf("потолок вызывающего назван разными величинами: процесс %d, профиль %d, страница %d",
			procSubject, chartSubject, pageSubject)
	}
}

// Процесс без ручек не создаёт больше потоков, чем примет КАЖДЫЙ владелец
// журнала дерева: иначе исчерпание наступает у владельца — у всех сразу.
func TestProcessStreamCeilingWithoutKnobsFitsEveryOwner(t *testing.T) {
	procReplica, procSubject := processDefaults(t)
	owners := ownerCeilings(t)
	if len(owners) == 0 {
		t.Fatal("владельцев журнала с объявленным потолком в дереве 0 — проба не исполнилась")
	}
	for f, ceiling := range owners {
		if procReplica > ceiling {
			t.Errorf("процесс без ручек держит %d потоков реплики, владелец %s принимает %d", procReplica, f, ceiling)
		}
	}
	if procSubject > procReplica {
		t.Errorf("потолок вызывающего %d больше потолка реплики %d", procSubject, procReplica)
	}
	t.Logf("перепись: владельцев с потолком %d · потолок процесса %d", len(owners), procReplica)
}
