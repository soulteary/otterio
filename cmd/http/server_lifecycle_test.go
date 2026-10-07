package http

import (
	"bufio"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func startTestServer(t *testing.T, server *Server) string {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- server.Start()
		close(done)
	}()
	t.Cleanup(func() {
		_ = server.Shutdown()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("listener did not stop")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		server.listenerMutex.Lock()
		listener := server.listener
		server.listenerMutex.Unlock()
		if listener != nil {
			return listener.Addr().String()
		}
		select {
		case err := <-done:
			t.Fatalf("server did not start: %v", err)
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("listener did not start")
	return ""
}

func TestHTTPListenerCloseUnblocksAccept(t *testing.T) {
	listener := &httpListener{acceptCh: make(chan acceptResult), doneCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		done <- err
	}()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Accept returned %v, want net.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Accept remained blocked after Close")
	}
	if _, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after Close returned %v", err)
	}
}

type blockedResponseStream struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockedResponseStream) Read([]byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return 0, io.EOF
}

func TestServerCountsRegisteredRoutesAndResponseStreams(t *testing.T) {
	for _, mode := range []string{"handler", "stream", "stream-writer"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			app := fiber.New()
			app.Get("/blocked", func(c fiber.Ctx) error {
				if mode == "stream" {
					return c.SendStream(&blockedResponseStream{entered: entered, release: release})
				}
				if mode == "stream-writer" {
					return c.SendStreamWriter(func(w *bufio.Writer) {
						close(entered)
						<-release
						_, _ = w.WriteString("done")
					})
				}
				close(entered)
				<-release
				return c.SendString("done")
			})
			server := NewServer([]string{"127.0.0.1:0"}, app, nil)
			address := startTestServer(t, server)
			clientDone := make(chan error, 1)
			go func() {
				client := &http.Client{Timeout: 5 * time.Second}
				response, err := client.Get("http://" + address + "/blocked")
				if err == nil {
					_, err = io.Copy(io.Discard, response.Body)
					response.Body.Close()
				}
				clientDone <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not enter the handler or stream")
			}
			if count := server.GetRequestCount(); count != 1 {
				t.Fatalf("active request count = %d, want 1", count)
			}
			unblock()
			if err := <-clientDone; err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for server.GetRequestCount() != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if count := server.GetRequestCount(); count != 0 {
				t.Fatalf("completed request count = %d, want 0", count)
			}
		})
	}
}

func TestShutdownClosesEveryListener(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[secure], func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error { return c.SendString("ready") })
			server := NewServer([]string{"127.0.0.1:0", "localhost:0"}, app, nil)
			scheme := "http"
			if secure {
				server = NewServer(server.Addrs, app, getCert)
				scheme = "https"
			}
			_ = startTestServer(t, server)
			transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			addresses := server.listener.Addrs()
			if len(addresses) != 2 {
				t.Fatalf("listener count = %d, want 2", len(addresses))
			}
			for _, address := range addresses {
				if address.(*net.TCPAddr).IP.IsUnspecified() {
					t.Fatalf("aggregate Addr replaced a real listener address: %s", address)
				}
			}
			for _, address := range addresses {
				response, err := client.Get(scheme + "://" + address.String() + "/")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
			if err := server.Shutdown(); err != nil {
				t.Fatal(err)
			}
			for _, address := range addresses {
				conn, err := net.DialTimeout("tcp", address.String(), time.Second)
				if err == nil {
					conn.Close()
					t.Errorf("listener %s still accepts connections after shutdown", address)
				}
			}
		})
	}
}

func TestShutdownIdleServerUsesOneDeadline(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error { return c.SendString("ready") })
	server := NewServer([]string{"127.0.0.1:0"}, app, nil)
	server.ShutdownTimeout = 250 * time.Millisecond
	address := startTestServer(t, server)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get("http://" + address + "/")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if err := server.Shutdown(); err != nil {
		t.Fatalf("idle shutdown failed: %v", err)
	}
}

func TestTLSServerNegotiatesSupportedHTTPProtocol(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error { return c.SendString("ready") })
	server := NewServer([]string{"127.0.0.1:0"}, app, getCert)
	address := startTestServer(t, server)
	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, // Test certificate is self-signed.
		ForceAttemptHTTP2: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Get("https://" + address + "/")
	if err != nil {
		t.Fatalf("HTTP/2-capable client could not reach server: %v", err)
	}
	defer response.Body.Close()
	if response.ProtoMajor != 1 || response.StatusCode != http.StatusOK {
		t.Fatalf("unsupported negotiated response: %s, status %d", response.Proto, response.StatusCode)
	}
}
