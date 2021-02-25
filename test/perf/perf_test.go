// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: Apache-2.0

package perf

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

type ByteSize int64

const (
	_           = iota
	KB ByteSize = 1 << (10 * iota)
	MB
	GB
)

func TestBenchmarkCopyingLargeImageWithinSameRegistryShouldBeFast(t *testing.T) {
	logger := Logger{}
	env := BuildEnv(t)
	defer env.Cleanup()
	perfTestingRepo := startRegistryForPerfTesting(t, env)

	benchmarkResultInitialPush := testing.Benchmark(func(b *testing.B) {
		env.ImageFactory.PushImage(perfTestingRepo, int64(GB))
	})

	benchmarkResultCopyInSameRegistry := testing.Benchmark(func(b *testing.B) {
		imgpkg := Imgpkg{b, logger, env.ImgpkgPath}

		imgpkg.Run([]string{"copy", "-i", perfTestingRepo, "--to-repo", perfTestingRepo + strconv.Itoa(b.N)})
	})

	logger.Debugf("initial push took: %v\n", benchmarkResultInitialPush.T)
	logger.Debugf("imgpkg copy took: %v\n", benchmarkResultCopyInSameRegistry.T)

	expectedMaxTimeToTake := benchmarkResultInitialPush.T.Nanoseconds() / 15
	actualTimeTaken := benchmarkResultCopyInSameRegistry.T.Nanoseconds()

	if actualTimeTaken > expectedMaxTimeToTake {
		t.Fatalf("copying a large image took too long. Expected it to take maximum [%v] but it took [%v]", time.Duration(expectedMaxTimeToTake), time.Duration(actualTimeTaken))
	}

}

func startRegistryForPerfTesting(t *testing.T, env *Env) string {
	dockerRunCmd := exec.Command("docker", "run", "-d", "-p", "5000", "--env", "REGISTRY_VALIDATION_MANIFESTS_URLS_ALLOW=- ^https?://", "--restart", "always", "--name", "registry-for-perf-testing", "registry:2")
	output, err := dockerRunCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("output: %s, %s", output, err)
	}

	env.AddCleanup(func() {
		exec.Command("docker", "stop", "registry-for-perf-testing").Run()
		exec.Command("docker", "rm", "-v", "registry-for-perf-testing").Run()
	})

	inspectCmd := exec.Command("docker", "inspect", `--format='{{(index (index .NetworkSettings.Ports "5000/tcp") 0).HostPort}}'`, "registry-for-perf-testing")
	output, err = inspectCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("output: %s, %s", output, err)
	}

	hostPort := strings.ReplaceAll(string(output), "'", "")
	return fmt.Sprintf("localhost:%s/repo/perf-image", strings.ReplaceAll(hostPort, "\n", ""))
}