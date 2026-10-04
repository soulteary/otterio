//go:build ignore
// +build ignore

/*
 * MinIO Cloud Storage, (C) 2015 MinIO, Inc.
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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

func genLDFlags(version string) string {
	commit := commitID()
	shortCommit := commit
	if len(shortCommit) > 12 {
		shortCommit = shortCommit[:12]
	}
	ldflagsStr := "-s -w"
	ldflagsStr += " -X github.com/soulteary/otterio/cmd.Version=" + version
	ldflagsStr += " -X github.com/soulteary/otterio/cmd.ReleaseTag=" + releaseTag(version)
	ldflagsStr += " -X github.com/soulteary/otterio/cmd.CommitID=" + commit
	ldflagsStr += " -X github.com/soulteary/otterio/cmd.ShortCommitID=" + shortCommit
	ldflagsStr += " -X github.com/soulteary/otterio/cmd.GOPATH=" + os.Getenv("GOPATH")
	ldflagsStr += " -X github.com/soulteary/otterio/cmd.GOROOT=" + os.Getenv("GOROOT")
	return ldflagsStr
}

// genReleaseTag prints release tag to the console for easy git tagging.
func releaseTag(version string) string {
	relPrefix := "DEVELOPMENT"
	if prefix := os.Getenv("OTTERIO_RELEASE"); prefix != "" {
		relPrefix = prefix
	}

	relSuffix := ""
	if hotfix := os.Getenv("OTTERIO_HOTFIX"); hotfix != "" {
		relSuffix = hotfix
	}

	relTag := strings.Replace(version, " ", "-", -1)
	relTag = strings.Replace(relTag, ":", "-", -1)
	relTag = strings.Replace(relTag, ",", "", -1)
	relTag = relPrefix + "." + relTag

	if relSuffix != "" {
		relTag += "." + relSuffix
	}

	return relTag
}

// commitID permits source-archive/container builds without copying .git. The
// override is metadata only: it never selects or downloads another source tree.
// "unknown" is explicit for local builds; ordinary release builds still use Git.
func commitID() string {
	if commit := os.Getenv("OTTERIO_BUILD_COMMIT"); commit != "" {
		if commit == "unknown" || regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(commit) {
			return commit
		}
		fmt.Fprintln(os.Stderr, "OTTERIO_BUILD_COMMIT must be a full lowercase Git SHA or unknown")
		os.Exit(1)
	}
	commit, err := exec.Command("git", "log", "--format=%H", "-n1").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error generating git commit-id: ", err)
		os.Exit(1)
	}
	return strings.TrimSpace(string(commit))
}

func main() {
	var version string
	if len(os.Args) > 1 {
		version = os.Args[1]
	} else {
		version = time.Now().UTC().Format(time.RFC3339)
	}

	fmt.Println(genLDFlags(version))
}
