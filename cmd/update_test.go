/*
 * MinIO Cloud Storage, (C) 2017 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

// Tests user agent string.
func TestUserAgent(t *testing.T) {
	testCases := []struct {
		envName     string
		envValue    string
		mode        string
		expectedStr string
	}{
		{
			envName:     "",
			envValue:    "",
			mode:        globalOtterioModeFS,
			expectedStr: fmt.Sprintf("OtterIO (%s; %s; %s; source) OtterIO/DEVELOPMENT.GOGET OtterIO/DEVELOPMENT.GOGET OtterIO/DEVELOPMENT.GOGET", runtime.GOOS, runtime.GOARCH, globalOtterioModeFS),
		},
		{
			envName:     "MESOS_CONTAINER_NAME",
			envValue:    "mesos-11111",
			mode:        globalOtterioModeErasure,
			expectedStr: fmt.Sprintf("OtterIO (%s; %s; %s; %s; source) OtterIO/DEVELOPMENT.GOGET OtterIO/DEVELOPMENT.GOGET OtterIO/DEVELOPMENT.GOGET OtterIO/universe-%s", runtime.GOOS, runtime.GOARCH, globalOtterioModeErasure, "dcos", "mesos-1111"),
		},
		{
			envName:     "KUBERNETES_SERVICE_HOST",
			envValue:    "10.11.148.5",
			mode:        globalOtterioModeErasure,
			expectedStr: fmt.Sprintf("OtterIO (%s; %s; %s; %s; source) OtterIO/DEVELOPMENT.GOGET OtterIO/DEVELOPMENT.GOGET OtterIO/DEVELOPMENT.GOGET", runtime.GOOS, runtime.GOARCH, globalOtterioModeErasure, "kubernetes"),
		},
	}

	for i, testCase := range testCases {
		sci := os.Getenv("OTTERIO_CI_CD")
		os.Setenv("OTTERIO_CI_CD", "")

		os.Setenv(testCase.envName, testCase.envValue)
		if testCase.envName == "MESOS_CONTAINER_NAME" {
			os.Setenv("MARATHON_APP_LABEL_DCOS_PACKAGE_VERSION", "mesos-1111")
		}
		str := getUserAgent(testCase.mode)
		expectedStr := testCase.expectedStr
		if IsDocker() {
			expectedStr = strings.Replace(expectedStr, "; source", "; docker; source", -1)
		}
		if str != expectedStr {
			t.Errorf("Test %d: expected: %s, got: %s", i+1, expectedStr, str)
		}
		os.Setenv("OTTERIO_CI_CD", sci)
		os.Unsetenv("MARATHON_APP_LABEL_DCOS_PACKAGE_VERSION")
		os.Unsetenv(testCase.envName)
	}
}

// Tests if the environment we are running is in DCOS.
func TestIsDCOS(t *testing.T) {
	sci := os.Getenv("OTTERIO_CI_CD")
	os.Setenv("OTTERIO_CI_CD", "")
	defer os.Setenv("OTTERIO_CI_CD", sci)

	os.Setenv("MESOS_CONTAINER_NAME", "mesos-1111")
	dcos := IsDCOS()
	if !dcos {
		t.Fatalf("Expected %t, got %t", true, dcos)
	}

	os.Unsetenv("MESOS_CONTAINER_NAME")
	dcos = IsDCOS()
	if dcos {
		t.Fatalf("Expected %t, got %t", false, dcos)
	}
}

// Tests if the environment we are running is in kubernetes.
func TestIsKubernetes(t *testing.T) {
	sci := os.Getenv("OTTERIO_CI_CD")
	os.Setenv("OTTERIO_CI_CD", "")
	defer os.Setenv("OTTERIO_CI_CD", sci)

	os.Setenv("KUBERNETES_SERVICE_HOST", "10.11.148.5")
	kubernetes := IsKubernetes()
	if !kubernetes {
		t.Fatalf("Expected %t, got %t", true, kubernetes)
	}
	os.Unsetenv("KUBERNETES_SERVICE_HOST")
	kubernetes = IsKubernetes()
	if kubernetes {
		t.Fatalf("Expected %t, got %t", false, kubernetes)
	}
}

// Tests if the environment we are running is Helm chart.
func TestGetHelmVersion(t *testing.T) {
	createTempFile := func(content string) string {
		tmpfile, err := os.CreateTemp("", "helm-testfile-")
		if err != nil {
			t.Fatalf("Unable to create temporary file. %s", err)
		}
		if _, err = tmpfile.Write([]byte(content)); err != nil {
			t.Fatalf("Unable to create temporary file. %s", err)
		}
		if err = tmpfile.Close(); err != nil {
			t.Fatalf("Unable to create temporary file. %s", err)
		}
		return tmpfile.Name()
	}

	filename := createTempFile(
		`app="virtuous-rat-otterio"
chart="otterio-0.1.3"
heritage="Tiller"
pod-template-hash="818089471"`)

	defer os.Remove(filename)

	testCases := []struct {
		filename       string
		expectedResult string
	}{
		{"", ""},
		{"/tmp/non-existing-file", ""},
		{filename, "otterio-0.1.3"},
	}

	for _, testCase := range testCases {
		result := getHelmVersion(testCase.filename)

		if testCase.expectedResult != result {
			t.Fatalf("result: expected: %v, got: %v", testCase.expectedResult, result)
		}
	}
}
