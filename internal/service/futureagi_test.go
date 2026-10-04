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
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
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
	if recorded["body"] == "" || !contains(recorded["body"], "project_name") {
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

func contains(body, needle string) bool {
	return len(body) >= len(needle) && (body == needle || len(body) > 0 && (indexOf(body, needle) >= 0))
}

func indexOf(body, needle string) int {
	for i := 0; i+len(needle) <= len(body); i++ {
		if body[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
