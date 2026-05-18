package web

import (
	"net/http/httptest"
	"testing"

	"ecs-controller/internal/config"
	"ecs-controller/internal/monitor"
)

func TestAuthorizedUsesLoginPasswordHeaderOnly(t *testing.T) {
	service := monitor.NewService(config.Config{
		Server: config.ServerConfig{Password: "secret"},
	}, nil)
	server := NewServer(service, "")

	headerRequest := httptest.NewRequest("GET", "/api/status", nil)
	headerRequest.Header.Set("X-Login-Password", "secret")
	if !server.authorized(headerRequest) {
		t.Fatal("header password was rejected")
	}

	queryRequest := httptest.NewRequest("GET", "/api/status?token=secret", nil)
	if server.authorized(queryRequest) {
		t.Fatal("query token should not authorize requests")
	}
}
