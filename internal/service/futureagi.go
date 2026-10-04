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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

const futureAGICollectorPath = "/tracer/v1/traces"

type futureAGICtxKeyType struct{}

var futureAGICtxKey = futureAGICtxKeyType{}

// FutureAGIClientFromTenant returns a tracing client for the tenant, or nil
// when Future AGI is not configured. A lookup failure is non-fatal: this is
// observability, not a chat requirement.
func FutureAGIClientFromTenant(ctx context.Context, tenantID string) *FutureAGIClient {
	if tenantID == "" || dao.DB == nil {
		return nil
	}
	var row entity.TenantFutureAGI
	err := dao.DB.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&row).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return nil
	}
	if row.Endpoint == "" || row.APIKey == "" || row.SecretKey == "" || row.ProjectName == "" {
		return nil
	}
	return NewFutureAGIClient(row.Endpoint, row.APIKey, row.SecretKey, row.ProjectName, row.Redact)
}

// FutureAGISpan is one chat turn, mapped onto the attributes fi-collector
// actually reads. It is not a Langfuse event.
type FutureAGISpan struct {
	TraceID          string
	SessionID        string
	TenantID         string
	Model            string
	Input            string
	Output           string
	PromptTokens     int
	CompletionTokens int
	Error            string
	Start            time.Time
	End              time.Time
}

// FutureAGIClient posts OTLP/HTTP traces for one tenant. Writes are async:
// a full channel drops the span and returns nil, so the chat path never
// waits on the collector.
type FutureAGIClient struct {
	Endpoint    string
	APIKey      string
	SecretKey   string
	ProjectName string
	Redact      bool
	HTTP        *http.Client

	events  chan []byte
	stop    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func NewFutureAGIClient(endpoint, apiKey, secretKey, projectName string, redact bool) *FutureAGIClient {
	c := &FutureAGIClient{
		Endpoint:    endpoint,
		APIKey:      apiKey,
		SecretKey:   secretKey,
		ProjectName: projectName,
		Redact:      redact,
		HTTP:        &http.Client{Timeout: 5 * time.Second},
		events:      make(chan []byte, 1024),
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
	}
	go c.worker()
	return c
}

func (c *FutureAGIClient) PostSpan(ctx context.Context, span FutureAGISpan) error {
	if c == nil {
		return fmt.Errorf("nil futureagi client")
	}
	body, err := json.Marshal(c.payload(span))
	if err != nil {
		return err
	}
	select {
	case c.events <- body:
		return nil
	default:
		return nil
	}
}

func (c *FutureAGIClient) payload(span FutureAGISpan) map[string]any {
	attributes := []map[string]any{
		stringAttr("gen_ai.operation.name", "chat"),
		stringAttr("session.id", span.SessionID),
		stringAttr("gen_ai.conversation.id", span.SessionID),
		stringAttr("user.id", span.TenantID),
		stringAttr("ragflow.tenant_id", span.TenantID),
	}
	if span.Model != "" {
		attributes = append(attributes,
			stringAttr("gen_ai.request.model", span.Model),
			stringAttr("llm.model_name", span.Model),
		)
	}
	if !c.Redact {
		if span.Input != "" {
			attributes = append(attributes, stringAttr("input.value", span.Input))
		}
		if span.Output != "" {
			attributes = append(attributes, stringAttr("output.value", span.Output))
		}
	}
	if span.PromptTokens == 0 && span.CompletionTokens == 0 {
		attributes = append(attributes, boolAttr("gen_ai.usage.unavailable", true))
	} else {
		attributes = append(attributes,
			intAttr("gen_ai.usage.input_tokens", span.PromptTokens),
			intAttr("gen_ai.usage.output_tokens", span.CompletionTokens),
		)
	}
	status := map[string]any{"code": 1}
	if span.Error != "" {
		status = map[string]any{"code": 2, "message": span.Error}
	}
	return map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []map[string]any{
						stringAttr("project_name", c.ProjectName),
						stringAttr("project_type", "observe"),
					},
				},
				"scopeSpans": []any{
					map[string]any{
						"scope": map[string]any{"name": "ragflow-futureagi"},
						"spans": []any{
							map[string]any{
								"traceId":           span.TraceID,
								"spanId":            span.TraceID[:16],
								"name":              "ragflow.chat",
								"startTimeUnixNano": fmt.Sprintf("%d", span.Start.UnixNano()),
								"endTimeUnixNano":   fmt.Sprintf("%d", span.End.UnixNano()),
								"attributes":        attributes,
								"status":            status,
							},
						},
					},
				},
			},
		},
	}
}

func (c *FutureAGIClient) worker() {
	defer close(c.stopped)
	for {
		select {
		case <-c.stop:
			return
		case body := <-c.events:
			c.post(context.Background(), body)
		}
	}
}

func (c *FutureAGIClient) post(ctx context.Context, body []byte) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, collectorEndpoint(c.Endpoint), bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.APIKey)
	req.Header.Set("X-Secret-Key", c.SecretKey)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
}

func (c *FutureAGIClient) Shutdown(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.once.Do(func() { close(c.stop) })
	select {
	case <-c.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func collectorEndpoint(endpoint string) string {
	trimmed := strings.TrimRight(endpoint, "/")
	suffix := strings.TrimRight(futureAGICollectorPath, "/")
	if strings.HasSuffix(trimmed, suffix) {
		return trimmed
	}
	return trimmed + futureAGICollectorPath
}

func stringAttr(key, value string) map[string]any {
	return map[string]any{"key": key, "value": map[string]any{"stringValue": value}}
}

func intAttr(key string, value int) map[string]any {
	return map[string]any{"key": key, "value": map[string]any{"intValue": fmt.Sprintf("%d", value)}}
}

func boolAttr(key string, value bool) map[string]any {
	return map[string]any{"key": key, "value": map[string]any{"boolValue": value}}
}

// futureAGISpan posts one chat span if the request context carries a client.
// It never returns an error to the chat path.
func futureAGISpan(ctx context.Context, sessionID, tenantID, model, input, output string, promptTokens, completionTokens int, chatErr string) {
	client, ok := ctx.Value(futureAGICtxKey).(*FutureAGIClient)
	if !ok || client == nil {
		return
	}
	now := time.Now()
	traceID := fmt.Sprintf("%016x%016x", uint64(now.UnixNano()), uint64(now.UnixNano()^0x9e3779b97f4a7c15))
	_ = client.PostSpan(ctx, FutureAGISpan{
		TraceID:          traceID,
		SessionID:        sessionID,
		TenantID:         tenantID,
		Model:            model,
		Input:            input,
		Output:           output,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		Error:            chatErr,
		Start:            now,
		End:              now,
	})
}
