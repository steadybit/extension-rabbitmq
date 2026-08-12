// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package clients

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListAllExchanges_PaginatesAndFiltersColumns(t *testing.T) {
	const total = 1200
	var sawColumns bool
	var requests int

	mux := http.NewServeMux()
	mux.HandleFunc("/api/exchanges", func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("columns") != "" {
			sawColumns = true
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		require.Greater(t, page, 0, "page parameter must be sent")
		require.Greater(t, pageSize, 0, "page_size parameter must be sent")

		start := (page - 1) * pageSize
		end := start + pageSize
		if end > total {
			end = total
		}
		items := make([]rabbithole.ExchangeInfo, 0, end-start)
		for i := start; i < end; i++ {
			items = append(items, rabbithole.ExchangeInfo{Vhost: "/", Name: fmt.Sprintf("ex-%d", i), Type: "topic"})
		}
		_ = json.NewEncoder(w).Encode(struct {
			Items      []rabbithole.ExchangeInfo `json:"items"`
			Page       int                       `json:"page"`
			PageCount  int                       `json:"page_count"`
			TotalCount int                       `json:"total_count"`
		}{Items: items, Page: page, PageCount: (total + pageSize - 1) / pageSize, TotalCount: total})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := ListAllExchanges(&config.ManagementEndpoint{URL: srv.URL, Username: "u", Password: "p"})
	require.NoError(t, err)
	assert.Len(t, got, total)
	assert.Equal(t, "ex-0", got[0].Name)
	assert.Equal(t, fmt.Sprintf("ex-%d", total-1), got[total-1].Name)
	assert.True(t, sawColumns, "columns filter must be sent")
	assert.Equal(t, 3, requests, "1200 exchanges at page size 500 need 3 requests")
}

func TestListAllExchanges_ErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := ListAllExchanges(&config.ManagementEndpoint{URL: srv.URL, Username: "u", Password: "p"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
