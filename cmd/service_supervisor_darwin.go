package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

const supervisorParentEnv = "_OTTERIO_SUPERVISOR_PARENT"
const restartWorkerExitCode = 75

func supervisedWorker() bool {
	return os.Getenv(supervisorParentEnv) == strconv.Itoa(os.Getppid())
}

// Darwin's runtime can stall in BeforeExec after a busy server has shut down.
// Keep a stable, lightweight public process and replace its worker with the
// standard process API. No runtime flags change and supervisors never nest.
func runServerSupervisor() {
	if supervisedWorker() {
		go watchSupervisor()
		return
	}
	os.Exit(superviseServer(os.Args))
}

func superviseServer(args []string) int {
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(signals)
	stopping := false
	for {
		// Resolve on every restart so an atomically replaced binary or symlink
		// starts the new version, as on the Unix exec path.
		path, err := exec.LookPath(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Cannot start server worker:", err)
			return 1
		}
		reader, writer, pipeErr := os.Pipe()
		if pipeErr != nil {
			fmt.Fprintln(os.Stderr, "Cannot monitor server worker:", pipeErr)
			return 1
		}
		command := exec.Command(path, args[1:]...)
		command.ExtraFiles = []*os.File{reader}
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, supervisorParentEnv+"=") {
				command.Env = append(command.Env, value)
			}
		}
		command.Env = append(command.Env, supervisorParentEnv+"="+strconv.Itoa(os.Getpid()))
		if err := command.Start(); err != nil {
			reader.Close()
			writer.Close()
			fmt.Fprintln(os.Stderr, "Cannot start server worker:", err)
			return 1
		}
		reader.Close()
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		for {
			select {
			case received := <-signals:
				stopping = true
				_ = command.Process.Signal(received)
			case err := <-done:
				writer.Close()
				if err == nil {
					return 0
				}
				if exit, ok := err.(*exec.ExitError); ok {
					code := exit.ExitCode()
					if code == restartWorkerExitCode && !stopping {
						goto restart
					}
					if code < 0 {
						if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
							return 128 + int(status.Signal())
						}
						return 1
					}
					return code
				}
				fmt.Fprintln(os.Stderr, "Server worker failed:", err)
				return 1
			}
		}
	restart:
	}
}

func restartSupervisedProcess() (bool, error) {
	if !supervisedWorker() {
		return true, fmt.Errorf("Darwin restart requires the server supervisor")
	}
	// HTTP listeners and storage were already closed by handleSignals.
	os.Exit(restartWorkerExitCode)
	return true, nil
}

// The parent retains the write end of fd 3's pipe. EOF also handles SIGKILL,
// which cannot be forwarded, so a forcibly killed supervisor leaves no server.
func watchSupervisor() {
	pipe := os.NewFile(3, "otterio-supervisor-liveness")
	if pipe != nil {
		_, _ = io.Copy(io.Discard, pipe)
		_ = pipe.Close()
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
}
