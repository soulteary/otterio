package http

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestShutdownBoundsActiveFiberRequest(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	app := fiber.New()
	app.Get("/blocked", func(c fiber.Ctx) error {
		close(entered)
		<-release
		return c.SendString("done")
	})
	server := NewServer([]string{"127.0.0.1:0"}, app, nil)
	server.ShutdownTimeout = 100 * time.Millisecond
	started := make(chan error, 1)
	go func() { started <- server.Start() }()
	deadline := time.Now().Add(5 * time.Second)
	address := ""
	for time.Now().Before(deadline) {
		server.listenerMutex.Lock()
		if server.listener != nil {
			address = server.listener.Addr().String()
		}
		server.listenerMutex.Unlock()
		if address != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if address == "" {
		t.Fatal("listener did not start")
	}
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + address + "/blocked")
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("request did not start")
	}
	begin := time.Now()
	err := server.Shutdown()
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected drain deadline, got %v", err)
	}
	if elapsed := time.Since(begin); elapsed > 2*time.Second {
		t.Fatalf("shutdown exceeded its bound: %v", elapsed)
	}
	select {
	case <-clientDone:
	case <-time.After(5 * time.Second):
		t.Fatal("client not released")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("listener not released")
	}
}
