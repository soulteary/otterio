//go:build ignore

// Copyright (C) 2026 soulteary, https://github.com/soulteary/otterio
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
)

// Run against the production binary so directory enumeration during subsystem
// initialization is covered in addition to the focused filesystem unit tests.
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run buildscripts/verify-s3-startup.go <otterio-binary>")
		os.Exit(1)
	}
	if err := verify(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Server startup and S3 create/put/get/delete checks passed")
}

func verify(binary string) (err error) {
	binary, err = filepath.Abs(binary)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "otterio-startup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	logFile, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	endpoint := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const accessKey, secretKey = "otterio-smoke-test", "otterio-smoke-test-secret"
	server := exec.CommandContext(ctx, binary, "server", "--address", endpoint, filepath.Join(dir, "data"))
	server.Env = append(os.Environ(), "OTTERIO_ACCESS_KEY="+accessKey, "OTTERIO_SECRET_KEY="+secretKey, "OTTERIO_BROWSER=off")
	server.Stdout, server.Stderr = logFile, logFile
	if err := server.Start(); err != nil {
		return err
	}
	defer func() {
		_ = server.Process.Kill()
		_ = server.Wait()
		if err != nil {
			if output, readErr := os.ReadFile(logFile.Name()); readErr == nil {
				fmt.Fprintf(os.Stderr, "Server output:\n%s\n", output)
			}
		}
	}()
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return err
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		attempt, cancelAttempt := context.WithTimeout(ctx, 2*time.Second)
		_, listErr := client.ListBuckets(attempt)
		cancelAttempt()
		if listErr == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("server did not initialize: %w (last S3 error: %v)", ctx.Err(), listErr)
		case <-ticker.C:
		}
	}
	const bucket, object, payload = "startup-test", "object.txt", "OtterIO S3 startup test\n"
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}
	if _, err := client.PutObject(ctx, bucket, object, strings.NewReader(payload), int64(len(payload)), minio.PutObjectOptions{}); err != nil {
		return fmt.Errorf("upload object: %w", err)
	}
	reader, err := client.GetObject(ctx, bucket, object, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("download object: %w", err)
	}
	contents, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return fmt.Errorf("read object: %w", readErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if string(contents) != payload {
		return fmt.Errorf("downloaded object does not match uploaded content: %q", contents)
	}
	if err := client.RemoveObject(ctx, bucket, object, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	if err := client.RemoveBucket(ctx, bucket); err != nil {
		return fmt.Errorf("delete bucket: %w", err)
	}
	return nil
}
