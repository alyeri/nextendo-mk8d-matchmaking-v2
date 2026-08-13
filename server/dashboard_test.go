package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

func TestPrometheusMetricsExposeCoreSubsystems(t *testing.T) {
	endpoint := nex.NewEndpoint(nex.NewSwitchSettings("test-key", 40000))
	matchmaking := nex.NewMatchmaking()
	recorder := httptest.NewRecorder()

	writePrometheusMetrics(recorder, endpoint, matchmaking)

	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("content type = %q", got)
	}
	body := recorder.Body.String()
	for _, metric := range []string{
		"nextendo_mk8d_connected_players",
		"nextendo_mk8d_matchmaking_gatherings_created_total",
		"nextendo_mk8d_reconnect_recovered_total",
		"nextendo_mk8d_lobbies_by_phase{phase=\"searching\"}",
		"nextendo_mk8d_nncs_valid_requests_total",
	} {
		if !strings.Contains(body, metric) {
			t.Errorf("metrics output does not contain %q", metric)
		}
	}
}

func TestDashboardRequiresBearerAndKickRequiresPost(t *testing.T) {
	endpoint := nex.NewEndpoint(nex.NewSwitchSettings("test-key", 40000))
	matchmaking := nex.NewMatchmaking()
	handler := dashboardMux(endpoint, matchmaking, nil, "secret-token")

	legacy := httptest.NewRecorder()
	handler.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/api/stats?key=secret-token", nil))
	if legacy.Code != http.StatusForbidden {
		t.Fatalf("legacy query token status = %d", legacy.Code)
	}

	authedRequest := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	authedRequest.Header.Set("Authorization", "Bearer secret-token")
	authed := httptest.NewRecorder()
	handler.ServeHTTP(authed, authedRequest)
	if authed.Code != http.StatusOK {
		t.Fatalf("Bearer request status = %d", authed.Code)
	}

	kickGet := httptest.NewRequest(http.MethodGet, "/api/kick", nil)
	kickGet.Header.Set("Authorization", "Bearer secret-token")
	kickGetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(kickGetRecorder, kickGet)
	if kickGetRecorder.Code != http.StatusMethodNotAllowed || kickGetRecorder.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET kick status=%d allow=%q", kickGetRecorder.Code, kickGetRecorder.Header().Get("Allow"))
	}

	kickPost := httptest.NewRequest(http.MethodPost, "/api/kick", strings.NewReader(`{"pid":1800000001}`))
	kickPost.Header.Set("Authorization", "Bearer secret-token")
	kickPost.Header.Set("Content-Type", "application/json")
	kickPostRecorder := httptest.NewRecorder()
	handler.ServeHTTP(kickPostRecorder, kickPost)
	if kickPostRecorder.Code != http.StatusOK {
		t.Fatalf("POST kick status=%d body=%s", kickPostRecorder.Code, kickPostRecorder.Body.String())
	}
}

func TestRoomsResponseReportsRedisDisabled(t *testing.T) {
	matchmaking := nex.NewMatchmaking()
	response := buildRoomsResponse(matchmaking, nil)
	if response.SchemaVersion != 1 || len(response.Rooms) != 0 {
		t.Fatalf("unexpected rooms response: %+v", response)
	}
	if enabled, _ := response.Redis["enabled"].(bool); enabled {
		t.Fatalf("Redis unexpectedly enabled: %+v", response.Redis)
	}
}
