package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
)

// SearchExperimentLogs 的过滤与游标行为:关键词(含大小写与 LIKE 转义)、
// 仅失败开关、原始行游标推进,以及与 ExperimentLogs 一致的范围圈定。
func TestSearchExperimentLogsFiltersAndCursor(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "Agent A", Conditions: benchmark.Conditions{Agent: "a"}})
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now().Add(-time.Hour)
	save := func(id, method, path, reqBody, respBody string, status int, errMsg string) {
		t.Helper()
		at = at.Add(time.Minute)
		entry := newTestLog(id, at)
		entry.TraceID = "task"
		entry.Method = method
		entry.Path = path
		entry.RequestBody = reqBody
		entry.ResponseBody = respBody
		entry.StatusCode = status
		entry.Error = errMsg
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	save("flag", "POST", "/v1/chat/completions", "", `{"flag captured from target alpha"}`, 200, "")
	save("fail", "POST", "/v1/messages", "", "upstream error", 500, "boom timeout")
	save("plain", "POST", "/v1/chat/completions", "user asks about weather", "normal answer", 200, "")
	save("nonmodel", "POST", "/api/upload", "", "uploaded", 500, "")
	save("getreq", "GET", "/v1/chat/completions", "", "", 200, "")
	save("percent", "POST", "/v1/chat/completions", "", "done 100%", 200, "")

	// trace 必须已有日志,运行才能关联
	run.TraceIDs = []string{"task"}
	if _, err = r.SaveExperimentRun(run); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ids := func(rows []*RequestLog) []string {
		out := []string{}
		for _, row := range rows {
			out = append(out, row.ID)
		}
		return out
	}

	// 无过滤:全部 POST 原始行(含非模型路径,SupportedPath 过滤是调用方的职责),不含 GET
	rows, err := r.SearchExperimentLogs(ctx, run.ID, "", false, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rows); len(got) != 5 || got[0] != "flag" || got[4] != "percent" {
		t.Fatalf("unfiltered rows: %v", got)
	}

	// 关键词命中响应正文预览;ASCII 大小写不敏感
	rows, err = r.SearchExperimentLogs(ctx, run.ID, "flag", false, 50, 0)
	if err != nil || len(rows) != 1 || rows[0].ID != "flag" {
		t.Fatalf("keyword match: %v %v", ids(rows), err)
	}
	if !strings.Contains(rows[0].ResponseBody, "flag captured") {
		t.Fatalf("preview missing: %q", rows[0].ResponseBody)
	}
	rows, err = r.SearchExperimentLogs(ctx, run.ID, "FLAG", false, 50, 0)
	if err != nil || len(rows) != 1 || rows[0].ID != "flag" {
		t.Fatalf("case-insensitive match: %v %v", ids(rows), err)
	}

	// 关键词命中错误文本
	rows, err = r.SearchExperimentLogs(ctx, run.ID, "timeout", false, 50, 0)
	if err != nil || len(rows) != 1 || rows[0].ID != "fail" {
		t.Fatalf("error text match: %v %v", ids(rows), err)
	}

	// 仅失败
	rows, err = r.SearchExperimentLogs(ctx, run.ID, "", true, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rows); len(got) != 2 || got[0] != "fail" || got[1] != "nonmodel" {
		t.Fatalf("errors only: %v", got)
	}

	// 组合过滤
	rows, err = r.SearchExperimentLogs(ctx, run.ID, "timeout", true, 50, 0)
	if err != nil || len(rows) != 1 || rows[0].ID != "fail" {
		t.Fatalf("combined filter: %v %v", ids(rows), err)
	}

	// 原始行游标
	page, err := r.SearchExperimentLogs(ctx, run.ID, "", false, 2, 0)
	if err != nil || len(page) != 2 || page[0].ID != "flag" || page[1].ID != "fail" {
		t.Fatalf("cursor page 1: %v %v", ids(page), err)
	}
	page, err = r.SearchExperimentLogs(ctx, run.ID, "", false, 2, 2)
	if err != nil || len(page) != 2 || page[0].ID != "plain" || page[1].ID != "nonmodel" {
		t.Fatalf("cursor page 2: %v %v", ids(page), err)
	}
	page, err = r.SearchExperimentLogs(ctx, run.ID, "", false, 2, 4)
	if err != nil || len(page) != 1 || page[0].ID != "percent" {
		t.Fatalf("cursor last page: %v %v", ids(page), err)
	}
	page, err = r.SearchExperimentLogs(ctx, run.ID, "", false, 2, 5)
	if err != nil || len(page) != 0 {
		t.Fatalf("cursor past end: %v %v", ids(page), err)
	}

	// LIKE 转义:百分号按字面匹配,不当作通配符
	rows, err = r.SearchExperimentLogs(ctx, run.ID, "100%", false, 50, 0)
	if err != nil || len(rows) != 1 || rows[0].ID != "percent" {
		t.Fatalf("literal percent: %v %v", ids(rows), err)
	}
}
