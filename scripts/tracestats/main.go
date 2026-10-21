// 一次性诊断工具：对指定调用链按生产代码路径分解耗时
// （指纹查询 → 行读取 → 时间线全量构建 → 派生/序列化 → 流程图/标签），
// 定位「流程图 7-8 秒」的服务端成本来源。
// 用法：go run ./scripts/tracestats <traceID>
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/shaomian/PrismCat/internal/timeline"
	_ "modernc.org/sqlite"
)

func main() {
	traceID := "cherrytest"
	if len(os.Args) > 1 {
		traceID = os.Args[1]
	}
	db, err := sql.Open("sqlite", "file:data/prismcat.db?mode=ro")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// 1. 指纹查询 ×100
	start := time.Now()
	var fpCount, fpLast int64
	for i := 0; i < 100; i++ {
		if err := db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(created_at_unix_ms),0)
			FROM request_logs WHERE trace_id = ?`, traceID).Scan(&fpCount, &fpLast); err != nil {
			panic(err)
		}
	}
	fmt.Printf("fingerprint x100      : %v (count=%d)\n", time.Since(start), fpCount)

	// 2. GetTraceRequests 等价查询（LEFT JOIN 注解）
	start = time.Now()
	rows, err := db.Query(`
	SELECT l.id, l.created_at, l.upstream, l.upstream_target, l.target_url, l.method, l.path, l.query,
		l.request_headers, l.request_body, l.request_body_original, l.request_body_final, l.request_body_ref, l.request_body_size,
		l.status_code, l.response_headers, l.response_body, l.response_body_ref, l.response_body_size,
		l.streaming, l.latency_ms, l.error, l.truncated, l.tag,
		l.request_override_applied, l.request_override_rules, l.request_override_error,
		l.request_header_override_applied, l.request_header_override_changes, l.request_headers_original,
		l.trace_id, l.parent_log_id, l.trace_seq,
		l.usage_input_tokens, l.usage_output_tokens, l.usage_total_tokens, l.usage_raw, l.usage_source
	FROM request_logs l LEFT JOIN log_annotations a ON a.log_id = l.id
	WHERE l.trace_id = ? ORDER BY l.trace_seq, l.created_at_unix_ms`, traceID)
	if err != nil {
		panic(err)
	}
	type row struct {
		logID        string
		createdAtMs  int64
		latency      int64
		status       int
		errText      string
		upstream     string
		path         string
		streaming    bool
		reqBody      string
		respBody     string
		reqHeaders   string
		respHeaders  string
		traceSeq     int
		usageIn      sql.NullInt64
		usageOut     sql.NullInt64
	}
	var logs []row
	for rows.Next() {
		var r row
		var reqHeaders, respHeaders string
		var streaming int
		var truncated int
		var overrideApplied int
		var headerOverrideApplied int
		var headerOverrideChanges, reqHeadersOriginal string
		var overrideRules string
		var reqBody, reqBodyOrig, reqBodyFinal, reqBodyRef string
		var reqSize int64
		var respBody, respRef string
		var respSize int64
		var errText sql.NullString
		var tag sql.NullString
		var usageRaw, usageSource sql.NullString
		var createdAt string
		var upstreamTarget, targetURL, method, path, query string
		var traceIDCol, parentLogID string
		var usageTotal sql.NullInt64
		_ = rows.Scan(&r.logID, &createdAt, &r.upstream, &upstreamTarget, &targetURL, &method, &path, &query,
			&reqHeaders, &reqBody, &reqBodyOrig, &reqBodyFinal, &reqBodyRef, &reqSize,
			&r.status, &respHeaders, &respBody, &respRef, &respSize,
			&streaming, &r.latency, &errText, &truncated, &tag,
			&overrideApplied, &overrideRules, &r.errText,
			&headerOverrideApplied, &headerOverrideChanges, &reqHeadersOriginal,
			&traceIDCol, &parentLogID, &r.traceSeq,
			&r.usageIn, &r.usageOut, &usageTotal, &usageRaw, &usageSource)
		_ = createdAt
		r.reqBody = reqBody
		r.respBody = respBody
		r.reqHeaders = reqHeaders
		r.respHeaders = respHeaders
		r.streaming = streaming == 1
		logs = append(logs, r)
	}
	rows.Close()
	fmt.Printf("GetTraceRequests rows : %v (%d rows)\n", time.Since(start), len(logs))

	// 3. timeline.Build（不截断，批次 C 的缓存全量构建）
	inputs := make([]timeline.Input, 0, len(logs))
	for _, r := range logs {
		inputs = append(inputs, timeline.Input{
			LogID:       r.logID,
			Seq:         r.traceSeq,
			CreatedAtMs: r.createdAtMs,
			LatencyMs:   r.latency,
			StatusCode:  r.status,
			Streaming:   r.streaming,
			Path:        r.path,
			Upstream:    r.upstream,
			RequestBody: r.reqBody,
			ResponseBody: r.respBody,
		})
	}
	start = time.Now()
	full := timeline.Build(traceID, inputs, timeline.Options{MaxEventChars: 1 << 62})
	fmt.Printf("timeline.Build (full) : %v\n", time.Since(start))

	// 4. 默认级别派生（2000 截断）
	start = time.Now()
	def := timeline.TruncateEvents(full, timeline.DefaultEventChars)
	fmt.Printf("TruncateEvents(2000)  : %v\n", time.Since(start))

	// 5. 时间线响应序列化
	start = time.Now()
	defJSON, err := json.Marshal(def)
	if err != nil {
		panic(err)
	}
	fmt.Printf("timeline JSON marshal : %v (%d bytes)\n", time.Since(start), len(defJSON))

	// 6. 流程图 + 标签 + 响应序列化
	start = time.Now()
	diagramSrc, labels := timeline.Diagram(def, timeline.DiagramOptions{})
	_, _ = timeline.Diagram(def, timeline.DiagramOptions{})
	fmt.Printf("Diagram() x2          : %v\n", time.Since(start))
	start = time.Now()
	diagramJSON, err := json.Marshal(map[string]any{"mermaid": diagramSrc, "labels": labels})
	if err != nil {
		panic(err)
	}
	fmt.Printf("diagram JSON marshal  : %v (%d bytes)\n", time.Since(start), len(diagramJSON))

	// 7. 详情响应（handleTraceDetail 返回全部日志）序列化体量
	start = time.Now()
	detailJSON, err := json.Marshal(logs)
	if err != nil {
		panic(err)
	}
	fmt.Printf("detail JSON marshal   : %v (%d bytes)\n", time.Since(start), len(detailJSON))

	// 8. 分析上下文格式化
	start = time.Now()
	_ = timeline.FormatText(full, timeline.FormatOptions{MaxEventChars: 60000, MaxTotalChars: 60000})
	fmt.Printf("FormatText(analysis)  : %v\n", time.Since(start))
}