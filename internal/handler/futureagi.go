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

package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"ragflow/internal/common"
	"ragflow/internal/service"
)

// FutureAGIService is the behaviour the handler depends on.
type FutureAGIService interface {
	SetAPIKey(ctx context.Context, tenantID string, req service.FutureAGISettings) (*service.FutureAGIInfo, common.ErrorCode, error)
	GetAPIKey(ctx context.Context, tenantID string) (*service.FutureAGIInfo, common.ErrorCode, string, error)
	DeleteAPIKey(ctx context.Context, tenantID string) (bool, common.ErrorCode, string, error)
}

// FutureAGIHandler handles /futureagi/api-key, the tenant's Future AGI trace
// export settings. It mirrors /langfuse/api-key.
type FutureAGIHandler struct {
	futureAGIService FutureAGIService
}

// NewFutureAGIHandler creates a handler over the given service.
func NewFutureAGIHandler(futureAGIService FutureAGIService) *FutureAGIHandler {
	return &FutureAGIHandler{futureAGIService: futureAGIService}
}

// NewFutureAGI creates a handler over the live service.
func NewFutureAGI() *FutureAGIHandler {
	return NewFutureAGIHandler(service.NewFutureAGIService())
}

// SetAPIKey handles POST/PUT /futureagi/api-key.
func (h *FutureAGIHandler) SetAPIKey(c *gin.Context) {
	user, errorCode, errorMessage := GetUser(c)
	if errorCode != common.CodeSuccess {
		common.ErrorWithCode(c, errorCode, errorMessage)
		return
	}
	var req service.FutureAGISettings
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ResponseWithCodeData(c, common.CodeDataError, nil, "Invalid request: "+err.Error())
		return
	}
	info, code, err := h.futureAGIService.SetAPIKey(c.Request.Context(), user.ID, req)
	if err != nil {
		common.ErrorWithCode(c, code, err.Error())
		return
	}
	common.SuccessWithData(c, info, "success")
}

// GetAPIKey handles GET /futureagi/api-key. Keys come back masked.
func (h *FutureAGIHandler) GetAPIKey(c *gin.Context) {
	user, errorCode, errorMessage := GetUser(c)
	if errorCode != common.CodeSuccess {
		common.ErrorWithCode(c, errorCode, errorMessage)
		return
	}
	info, code, message, err := h.futureAGIService.GetAPIKey(c.Request.Context(), user.ID)
	if err != nil {
		common.ResponseWithCodeData(c, code, nil, message)
		return
	}
	if info == nil {
		common.SuccessWithData(c, nil, message)
		return
	}
	common.ResponseWithCodeData(c, code, info, message)
}

// DeleteAPIKey handles DELETE /futureagi/api-key.
func (h *FutureAGIHandler) DeleteAPIKey(c *gin.Context) {
	user, errorCode, errorMessage := GetUser(c)
	if errorCode != common.CodeSuccess {
		common.ErrorWithCode(c, errorCode, errorMessage)
		return
	}
	ok, code, message, err := h.futureAGIService.DeleteAPIKey(c.Request.Context(), user.ID)
	if err != nil {
		common.ResponseWithCodeData(c, code, nil, message)
		return
	}
	if message != "" {
		common.SuccessWithData(c, nil, message)
		return
	}
	common.SuccessWithData(c, ok, "success")
}
