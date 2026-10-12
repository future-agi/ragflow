//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ragflow/internal/common"
)

func TestFutureAGIPayloadSetsProjectAndUsage(t *testing.T) {
	client := &FutureAGIClient{ProjectName: "proj", Redact: false}
	payload := client.payload(FutureAGISpan{
		TraceID:          "0123456789abcdef0123456789abcdef",
		SessionID:        "chat-1",
		TenantID:         "tenant-1",
		Model:            "gpt",
		Input:            "hello",
		PromptTokens:     3,
		CompletionTokens: 5,
		Start:            time.Unix(0, 0),
		End:              time.Unix(1, 0),
	})
	resource := payload["resourceSpans"].([]any)[0].(map[string]any)["resource"].(map[string]any)
	attrs := resource["attributes"].([]map[string]any)
	if attrs[0]["value"].(map[string]any)["stringValue"] != "proj" {
		t.Fatalf("project_name missing: %#v", attrs)
	}
	span := payload["resourceSpans"].([]any)[0].(map[string]any)["scopeSpans"].([]any)[0].(map[string]any)["spans"].([]any)[0].(map[string]any)
	if !hasAttr(span["attributes"].([]map[string]any), "gen_ai.request.model", "gpt") {
		t.Fatal("model attribute missing")
	}
	if !hasAttr(span["attributes"].([]map[string]any), "input.value", "hello") {
		t.Fatal("input missing when redact is off")
	}
}

func TestFutureAGIRedactDropsContent(t *testing.T) {
	client := &FutureAGIClient{ProjectName: "proj", Redact: true}
	payload := client.payload(FutureAGISpan{TraceID: "0123456789abcdef0123456789abcdef", Input: "secret", Output: "secret"})
	span := payload["resourceSpans"].([]any)[0].(map[string]any)["scopeSpans"].([]any)[0].(map[string]any)["spans"].([]any)[0].(map[string]any)
	for _, attr := range span["attributes"].([]map[string]any) {
		if attr["key"] == "input.value" || attr["key"] == "output.value" {
			t.Fatalf("redact left content on the span: %s", attr["key"])
		}
	}
}

func TestFutureAGIFullChannelDrops(t *testing.T) {
	client := &FutureAGIClient{events: make(chan []byte, 1), ProjectName: "proj"}
	client.events <- []byte("full")
	if err := client.PostSpan(t.Context(), FutureAGISpan{TraceID: "0123456789abcdef0123456789abcdef"}); err != nil {
		t.Fatalf("a full channel must not fail the caller: %v", err)
	}
}

func TestFutureAGIPostsCollectorHeaders(t *testing.T) {
	var got atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.Store(map[string]string{
			"path":   r.URL.Path,
			"key":    r.Header.Get("X-Api-Key"),
			"secret": r.Header.Get("X-Secret-Key"),
			"body":   string(body),
		})
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewFutureAGIClient(server.URL, "key", "secret", "proj", false)
	defer client.Shutdown(t.Context())
	if err := client.PostSpan(t.Context(), FutureAGISpan{
		TraceID: "0123456789abcdef0123456789abcdef",
		Model:   "gpt",
		Start:   time.Unix(0, 0),
		End:     time.Unix(1, 0),
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for got.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	recorded, _ := got.Load().(map[string]string)
	if recorded["path"] != futureAGICollectorPath {
		t.Fatalf("path %q", recorded["path"])
	}
	if recorded["key"] != "key" || recorded["secret"] != "secret" {
		t.Fatalf("headers %#v", recorded)
	}
	if recorded["body"] == "" || !strings.Contains(recorded["body"], "project_name") {
		t.Fatalf("body missing project_name: %s", recorded["body"])
	}
}

func TestCollectorEndpoint(t *testing.T) {
	if got := collectorEndpoint("https://api.futureagi.com"); got != "https://api.futureagi.com/tracer/v1/traces" {
		t.Fatal(got)
	}
	if got := collectorEndpoint("https://api.futureagi.com/tracer/v1/traces"); got != "https://api.futureagi.com/tracer/v1/traces" {
		t.Fatal(got)
	}
}

func hasAttr(attrs []map[string]any, key, value string) bool {
	for _, attr := range attrs {
		if attr["key"] == key && attr["value"].(map[string]any)["stringValue"] == value {
			return true
		}
	}
	return false
}

// collector records every OTLP body posted to it.
type collector struct {
	mu     sync.Mutex
	bodies []string
}

func (c *collector) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.bodies = append(c.bodies, string(body))
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (c *collector) posted() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.bodies...)
}

// TestFutureAGIShutdownPostsQueuedSpan pins that a span queued right before
// the request calls Shutdown is still posted. Both channels are ready when the
// worker starts, so without the drain select drops the span about half the time.
func TestFutureAGIShutdownPostsQueuedSpan(t *testing.T) {
	sink := &collector{}
	server := httptest.NewServer(http.HandlerFunc(sink.handler))
	defer server.Close()

	for i := 0; i < 20; i++ {
		client := &FutureAGIClient{
			Endpoint:    server.URL,
			ProjectName: "proj",
			HTTP:        server.Client(),
			events:      make(chan []byte, 1),
			stop:        make(chan struct{}),
			stopped:     make(chan struct{}),
		}
		if err := client.PostSpan(t.Context(), FutureAGISpan{TraceID: newFutureAGITraceID()}); err != nil {
			t.Fatal(err)
		}
		client.once.Do(func() { close(client.stop) })
		go client.worker()
		<-client.stopped
	}
	if got := len(sink.posted()); got != 20 {
		t.Fatalf("posted %d of 20 queued spans", got)
	}
}

// TestDecorateAnswerExportsFutureAGISpan pins the span the chat pipeline
// sends: the chat id as session, the model, the prompt with its query, the
// answer, and a start taken from the request timer.
func TestDecorateAnswerExportsFutureAGISpan(t *testing.T) {
	sink := &collector{}
	server := httptest.NewServer(http.HandlerFunc(sink.handler))
	defer server.Close()

	client := NewFutureAGIClient(server.URL, "key", "secret", "proj", false)
	client.ChatID = "chat-1"
	ctx := context.WithValue(t.Context(), futureAGICtxKey, client)
	timer := common.NewTimer()
	timer.Start()
	time.Sleep(20 * time.Millisecond)

	s := &ChatPipelineService{}
	s.decorateAnswer(ctx, "hello", map[string]interface{}{"chunks": []interface{}{}, "doc_aggs": []interface{}{}},
		"system prompt", []string{"question"}, 7, timer, nil, 0, false, nil, "",
		map[string]interface{}{"llm_name": "model-x"}, "tenant-1", nil, false)
	if err := client.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	bodies := sink.posted()
	if len(bodies) != 1 {
		t.Fatalf("want one span, got %d", len(bodies))
	}
	span := decodeSpan(t, bodies[0])
	attrs := spanAttrs(span)
	for key, want := range map[string]string{
		"session.id":     "chat-1",
		"user.id":        "tenant-1",
		"llm.model_name": "model-x",
		"output.value":   "hello",
	} {
		if attrs[key] != want {
			t.Errorf("%s = %q, want %q", key, attrs[key], want)
		}
	}
	if !strings.Contains(attrs["input.value"], "system prompt") || !strings.Contains(attrs["input.value"], "### Query:\nquestion") {
		t.Errorf("input.value = %q", attrs["input.value"])
	}
	start, _ := strconv.ParseInt(span["startTimeUnixNano"].(string), 10, 64)
	end, _ := strconv.ParseInt(span["endTimeUnixNano"].(string), 10, 64)
	if end-start < int64(20*time.Millisecond) {
		t.Errorf("span lasts %v, want at least the 20ms the timer ran", time.Duration(end-start))
	}
}

func TestFutureAGISpanCarriesTheChatError(t *testing.T) {
	client := &FutureAGIClient{ProjectName: "proj"}
	payload := client.payload(FutureAGISpan{TraceID: newFutureAGITraceID(), Error: "model timed out"})
	body, _ := json.Marshal(payload)
	status := decodeSpan(t, string(body))["status"].(map[string]any)
	if status["code"] != float64(2) || status["message"] != "model timed out" {
		t.Fatalf("status = %#v", status)
	}
}

func TestFutureAGITraceIDsAreRandom(t *testing.T) {
	a, b := newFutureAGITraceID(), newFutureAGITraceID()
	if len(a) != 32 || a == b {
		t.Fatalf("trace ids %q and %q", a, b)
	}
}

func decodeSpan(t *testing.T, body string) map[string]any {
	t.Helper()
	var payload struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []map[string]any `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.ResourceSpans[0].ScopeSpans[0].Spans[0]
}

func spanAttrs(span map[string]any) map[string]string {
	attrs := map[string]string{}
	for _, raw := range span["attributes"].([]any) {
		attr := raw.(map[string]any)
		if value, ok := attr["value"].(map[string]any)["stringValue"].(string); ok {
			attrs[attr["key"].(string)] = value
		}
	}
	return attrs
}
