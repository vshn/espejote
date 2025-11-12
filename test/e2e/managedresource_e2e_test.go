//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"os/exec"
	"path"

	corev1 "k8s.io/api/core/v1"
)

func (suite *E2ESuite) TestManagedResource() {
	mrNamespace := tmpNamespace(suite.T())
	suite.T().Log("Creating managed resource...")
	requireRun(suite.T(),
		exec.Command(
			"kubectl",
			"apply",
			"-n", mrNamespace,
			"-f", path.Join(suite.projectDir, "test", "e2e", "testdata", "managedresource", "basic.yaml"),
		),
	)
	suite.T().Log("Waiting for resource to become ready...")
	requireRun(suite.T(),
		exec.Command(
			"kubectl",
			"wait",
			"-n", mrNamespace,
			"managedresource.espejote.io/test-basic",
			"--for", "jsonpath={.status.status}=Ready",
			"--timeout", "1m",
		),
	)

	suite.T().Log("Creating subject cm...")
	requireRun(suite.T(),
		exec.Command(
			"kubectl",
			"create",
			"-n", mrNamespace,
			"configmap",
			"to-duplicate",
			"--from-literal=chuck=testa",
		),
	)
	requireRun(suite.T(),
		exec.Command(
			"kubectl",
			"label",
			"-n", mrNamespace,
			"configmap",
			"to-duplicate",
			"test-basic.espejote.io/duplicate-cm=true",
		),
	)

	suite.T().Log("Verifying duplicated cm...")
	requireRun(suite.T(),
		exec.Command(
			"kubectl",
			"wait",
			"-n", mrNamespace,
			"configmap/to-duplicate-duplicate",
			"--for=create",
			"--timeout", "1m",
		),
	)
	output := requireRun(suite.T(),
		exec.Command(
			"kubectl",
			"get",
			"-n", mrNamespace,
			"configmap/to-duplicate-duplicate",
			"-o", "json",
		),
	)
	var cm corev1.ConfigMap
	if suite.NoError(json.Unmarshal([]byte(output), &cm)) {
		suite.Equal("testa", cm.Data["chuck"])
		suite.Equal("some sample string", cm.Data["sample"])
	}
}
