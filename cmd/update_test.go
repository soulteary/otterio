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
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOtterioVersionToReleaseTime(t *testing.T) {
	testCases := []struct {
		version    string
		isOfficial bool
	}{
		{"2017-09-29T19:16:56Z", true},
		{"RELEASE.2017-09-29T19-16-56Z", false},
		{"DEVELOPMENT.GOGET", false},
	}
	for i, testCase := range testCases {
		_, err := otterioVersionToReleaseTime(testCase.version)
		if (err == nil) != testCase.isOfficial {
			t.Errorf("Test %d: Expected %v but got %v",
				i+1, testCase.isOfficial, err == nil)
		}
	}
}

func TestReleaseTagToNFromTimeConversion(t *testing.T) {
	utcLoc, _ := time.LoadLocation("")
	testCases := []struct {
		t      time.Time
		tag    string
		errStr string
	}{
		{time.Date(2017, time.September, 29, 19, 16, 56, 0, utcLoc),
			"RELEASE.2017-09-29T19-16-56Z", ""},
		{time.Date(2017, time.August, 5, 0, 0, 53, 0, utcLoc),
			"RELEASE.2017-08-05T00-00-53Z", ""},
		{time.Now().UTC(), "2017-09-29T19:16:56Z",
			"2017-09-29T19:16:56Z is not a valid release tag"},
		{time.Now().UTC(), "DEVELOPMENT.GOGET",
			"DEVELOPMENT.GOGET is not a valid release tag"},
	}
	for i, testCase := range testCases {
		if testCase.errStr != "" {
			got := releaseTimeToReleaseTag(testCase.t)
			if got != testCase.tag && testCase.errStr == "" {
				t.Errorf("Test %d: Expected %v but got %v", i+1, testCase.tag, got)
			}
		}
		tagTime, err := releaseTagToReleaseTime(testCase.tag)
		if err != nil && err.Error() != testCase.errStr {
			t.Errorf("Test %d: Expected %v but got %v", i+1, testCase.errStr, err.Error())
		}
		if err == nil && !tagTime.Equal(testCase.t) {
			t.Errorf("Test %d: Expected %v but got %v", i+1, testCase.t, tagTime)
		}
	}

}

func TestDownloadURL(t *testing.T) {
	sci := os.Getenv("OTTERIO_CI_CD")

	os.Setenv("OTTERIO_CI_CD", "")
	defer os.Setenv("OTTERIO_CI_CD", sci)

	otterioVersion1 := releaseTimeToReleaseTag(UTCNow())

	// By default this fork has no release URL configured, so there is no
	// download hint and updates are effectively disabled.
	sru := os.Getenv(envOtterioUpdateReleaseURL)
	os.Unsetenv(envOtterioUpdateReleaseURL)
	defer os.Setenv(envOtterioUpdateReleaseURL, sru)
	if durl := getDownloadURL(otterioVersion1); durl != "" {
		t.Errorf("Expected empty download URL when %s is unset, got %s", envOtterioUpdateReleaseURL, durl)
	}

	// With a release URL configured, the usual per-environment hints apply.
	os.Setenv(envOtterioUpdateReleaseURL, "https://example.com/otterio/release/")
	base := otterioReleaseBaseURL()

	durl := getDownloadURL(otterioVersion1)
	if IsDocker() {
		if durl != "docker pull soulteary/otterio:"+otterioVersion1 {
			t.Errorf("Expected %s, got %s", "docker pull soulteary/otterio:"+otterioVersion1, durl)
		}
	} else {
		if runtime.GOOS == "windows" {
			if durl != base+"otterio.exe" {
				t.Errorf("Expected %s, got %s", base+"otterio.exe", durl)
			}
		} else {
			if durl != base+"otterio" {
				t.Errorf("Expected %s, got %s", base+"otterio", durl)
			}
		}
	}

	os.Setenv("KUBERNETES_SERVICE_HOST", "10.11.148.5")
	durl = getDownloadURL(otterioVersion1)
	if durl != kubernetesDeploymentDoc {
		t.Errorf("Expected %s, got %s", kubernetesDeploymentDoc, durl)
	}
	os.Unsetenv("KUBERNETES_SERVICE_HOST")

	os.Setenv("MESOS_CONTAINER_NAME", "mesos-1111")
	durl = getDownloadURL(otterioVersion1)
	if durl != mesosDeploymentDoc {
		t.Errorf("Expected %s, got %s", mesosDeploymentDoc, durl)
	}
	os.Unsetenv("MESOS_CONTAINER_NAME")
}

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
		tmpfile, err := ioutil.TempFile("", "helm-testfile-")
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

func TestDownloadReleaseData(t *testing.T) {
	httpServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer httpServer1.Close()
	httpServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "fbe246edbd382902db9a4035df7dce8cb441357d otterio.RELEASE.2016-10-07T01-16-39Z")
	}))
	defer httpServer2.Close()
	httpServer3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "", http.StatusNotFound)
	}))
	defer httpServer3.Close()

	testCases := []struct {
		releaseChecksumURL string
		expectedResult     string
		expectedErr        error
	}{
		{httpServer1.URL, "", nil},
		{httpServer2.URL, "fbe246edbd382902db9a4035df7dce8cb441357d otterio.RELEASE.2016-10-07T01-16-39Z\n", nil},
		{httpServer3.URL, "", fmt.Errorf("Error downloading URL " + httpServer3.URL + ". Response: 404 Not Found")},
	}

	for _, testCase := range testCases {
		u, err := url.Parse(testCase.releaseChecksumURL)
		if err != nil {
			t.Fatal(err)
		}

		result, err := downloadReleaseURL(u, 1*time.Second, "")
		if testCase.expectedErr == nil {
			if err != nil {
				t.Fatalf("error: expected: %v, got: %v", testCase.expectedErr, err)
			}
		} else if err == nil {
			t.Fatalf("error: expected: %v, got: %v", testCase.expectedErr, err)
		} else if testCase.expectedErr.Error() != err.Error() {
			t.Fatalf("error: expected: %v, got: %v", testCase.expectedErr, err)
		}

		if testCase.expectedResult != result {
			t.Fatalf("result: expected: %v, got: %v", testCase.expectedResult, result)
		}
	}
}

func TestParseReleaseData(t *testing.T) {
	releaseTime, _ := releaseTagToReleaseTime("RELEASE.2016-10-07T01-16-39Z")
	testCases := []struct {
		data                string
		expectedResult      time.Time
		expectedSha256hex   string
		expectedReleaseInfo string
		expectedErr         bool
	}{
		{"more than two fields", time.Time{}, "", "", true},
		{"more than", time.Time{}, "", "", true},
		{"more than.two.fields", time.Time{}, "", "", true},
		{"more otterio.RELEASE.fields", time.Time{}, "", "", true},
		{"more otterio.RELEASE.2016-10-07T01-16-39Z", time.Time{}, "", "", true},
		{"fbe246edbd382902db9a4035df7dce8cb441357d otterio.RELEASE.2016-10-07T01-16-39Z\n", releaseTime, "fbe246edbd382902db9a4035df7dce8cb441357d",
			"otterio.RELEASE.2016-10-07T01-16-39Z", false},
		{"fbe246edbd382902db9a4035df7dce8cb441357d otterio.RELEASE.2016-10-07T01-16-39Z.customer-hotfix\n", releaseTime, "fbe246edbd382902db9a4035df7dce8cb441357d",
			"otterio.RELEASE.2016-10-07T01-16-39Z.customer-hotfix", false},
	}

	for i, testCase := range testCases {
		sha256Sum, result, releaseInfo, err := parseReleaseData(testCase.data)
		if !testCase.expectedErr {
			if err != nil {
				t.Errorf("error case %d: expected no error, got: %v", i+1, err)
			}
		} else if err == nil {
			t.Errorf("error case %d: expected error got: %v", i+1, err)
		}
		if err == nil {
			if hex.EncodeToString(sha256Sum) != testCase.expectedSha256hex {
				t.Errorf("case %d: result: expected: %v, got: %x", i+1, testCase.expectedSha256hex, sha256Sum)
			}
			if !testCase.expectedResult.Equal(result) {
				t.Errorf("case %d: result: expected: %v, got: %v", i+1, testCase.expectedResult, result)
			}
			if testCase.expectedReleaseInfo != releaseInfo {
				t.Errorf("case %d: result: expected: %v, got: %v", i+1, testCase.expectedReleaseInfo, releaseInfo)
			}
		}
	}
}

// TestDoUpdateRejectsLocalFileSource guards the fix for CVE-2022-35919: the
// server update path must never read a local filesystem path as an update
// source, otherwise an admin authorized for admin:ServerUpdate could read
// arbitrary files (e.g. "mc admin update alias/ /etc/passwd").
func TestDoUpdateRejectsLocalFileSource(t *testing.T) {
	secret := "TOP-SECRET-ROOT-PASSWORD-should-never-be-read"
	f, err := ioutil.TempFile("", "otterio-update-traversal-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(secret); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// A bare path (no http/https scheme), as produced by "mc admin update <path>".
	u := &url.URL{Path: f.Name()}

	err = doUpdate(u, time.Now().UTC(), nil, "", "")
	if err == nil {
		t.Fatal("expected doUpdate to reject a local filesystem update source, got nil error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("update error leaked local file contents: %v", err)
	}
	if !strings.Contains(err.Error(), "only http and https URLs are supported") {
		t.Fatalf("expected unsupported-scheme error, got: %v", err)
	}
}
