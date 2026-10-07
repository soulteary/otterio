package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDarwinRestartSupervisor(t *testing.T) {
	if root := os.Getenv("OTTERIO_SUPERVISOR_TEST_DIR"); root != "" {
		if !supervisedWorker() {
			if err := os.WriteFile(filepath.Join(root, "parent"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
				panic(err)
			}
		}
		runServerSupervisor()
		parent, _ := os.ReadFile(filepath.Join(root, "parent"))
		if string(parent) != os.Getenv(supervisorParentEnv) {
			panic("public process changed")
		}
		if os.Getenv("OTTERIO_SUPERVISOR_TEST_SIGNAL") == "1" {
			signals := make(chan os.Signal, 1)
			signal.Notify(signals, syscall.SIGTERM)
			os.WriteFile(filepath.Join(root, "ready"), []byte(strconv.Itoa(os.Getpid())), 0600)
			received := <-signals
			os.WriteFile(filepath.Join(root, "signal"), []byte(received.String()), 0600)
			os.Exit(0)
		}
		file := filepath.Join(root, "count")
		data, _ := os.ReadFile(file)
		count, _ := strconv.Atoi(string(data))
		os.WriteFile(file, []byte(strconv.Itoa(count+1)), 0600)
		if count < 2 {
			if err := restartProcess(); err != nil {
				panic(err)
			}
			panic("restart returned")
		}
		fmt.Println("restart-ok")
		os.Exit(0)
	}
	for _, mode := range []string{"restart", "term", "parent-death"} {
		t.Run(mode, func(t *testing.T) {
			forwarding := mode != "restart"
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDarwinRestartSupervisor$")
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "GODEBUG=") && !strings.HasPrefix(value, "OTTERIO_SUPERVISOR_TEST_") && !strings.HasPrefix(value, supervisorParentEnv+"=") {
					command.Env = append(command.Env, value)
				}
			}
			command.Env = append(command.Env, "OTTERIO_SUPERVISOR_TEST_DIR="+root)
			if forwarding {
				command.Env = append(command.Env, "OTTERIO_SUPERVISOR_TEST_SIGNAL=1")
				if err := command.Start(); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(10 * time.Second)
				for {
					if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
						break
					}
					if time.Now().After(deadline) {
						command.Process.Kill()
						command.Wait()
						t.Fatal("worker not ready")
					}
					time.Sleep(10 * time.Millisecond)
				}
				signalToSend := syscall.SIGTERM
				if mode == "parent-death" {
					signalToSend = syscall.SIGKILL
				}
				if err := command.Process.Signal(signalToSend); err != nil {
					t.Fatal(err)
				}
				if err := command.Wait(); err != nil && mode != "parent-death" {
					t.Fatal(err)
				}
				deadline = time.Now().Add(5 * time.Second)
				for {
					if data, err := os.ReadFile(filepath.Join(root, "signal")); err == nil && len(data) > 0 {
						break
					}
					if time.Now().After(deadline) {
						data, _ := os.ReadFile(filepath.Join(root, "ready"))
						pid, _ := strconv.Atoi(string(data))
						if pid > 0 {
							_ = syscall.Kill(pid, syscall.SIGKILL)
						}
						t.Fatal("worker did not stop with supervisor")
					}
					time.Sleep(10 * time.Millisecond)
				}
				received, _ := os.ReadFile(filepath.Join(root, "signal"))
				if string(received) != "terminated" {
					t.Fatalf("signal was not forwarded: %q", received)
				}
			} else {
				output, err := command.CombinedOutput()
				if err != nil || !strings.Contains(string(output), "restart-ok") {
					t.Fatalf("restart failed: %v %s", err, output)
				}
				count, _ := os.ReadFile(filepath.Join(root, "count"))
				if string(count) != "3" {
					t.Fatalf("worker count: %s", count)
				}
			}
		})
	}
}
