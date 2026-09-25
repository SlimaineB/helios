package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/slim/ansiblehub/server/internal/model"
)

func nowCtx(ctx context.Context) time.Time { return time.Now().UTC() }

func encodeSummary(m map[string]int) string {
	if m == nil {
		m = map[string]int{}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func decodeSummary(s string) map[string]int {
	m := map[string]int{}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

func encodeTasks(t []model.ChangedTask) string {
	if t == nil {
		t = []model.ChangedTask{}
	}
	b, _ := json.Marshal(t)
	return string(b)
}

func decodeTasks(s string) []model.ChangedTask {
	var out []model.ChangedTask
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		out = []model.ChangedTask{}
	}
	return out
}

func encodeResources(r []model.Resource) string {
	if r == nil {
		r = []model.Resource{}
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func decodeResources(s string) []model.Resource {
	var out []model.Resource
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		out = []model.Resource{}
	}
	return out
}
