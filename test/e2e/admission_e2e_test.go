//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

func (suite *E2ESuite) TestAdmission() {
	admNamespace := tmpNamespace(suite.T())
	suite.T().Log("Creating managed resource...")
	requireRun(suite.T(),
		exec.Command(
			"kubectl",
			"apply",
			"-n", admNamespace,
			"-f", path.Join(suite.projectDir, "test", "e2e", "testdata", "admission", "basic.yaml"),
		),
	)

	suite.T().Log("Waiting for webhook to become ready...")
	suite.Require().EventuallyWithT(func(t *assert.CollectT) {
		cmd := exec.Command("kubectl", "create", "-f-", "-n", admNamespace, "-o=json")
		out := new(bytes.Buffer)
		cmd.Stdout = out
		cmd.Stdin = strings.NewReader(`
apiVersion: v1
kind: ConfigMap
metadata:
  generateName: wait-webhook-
`)
		require.NoError(t, cmd.Run())
		var createdCM corev1.ConfigMap
		require.NoError(t, json.Unmarshal(out.Bytes(), &createdCM))
		require.NotEmpty(t, createdCM.ObjectMeta.Annotations)
		require.NotEmpty(t, createdCM.ObjectMeta.Annotations["creator"])
	}, 1*time.Minute, 100*time.Microsecond, "Waiting for webhook to become ready and installed")

	suite.T().Log("Creating subject cm...")
	requireRun(suite.T(),
		exec.Command(
			"kubectl",
			"create",
			"-n", admNamespace,
			"configmap",
			"my-cm",
			"--from-literal=chuck=testa",
		),
	)
	suite.T().Log("Verifying annotation rejection...")
	cmd := exec.Command(
		"kubectl",
		"annotate",
		"-n", admNamespace,
		"configmap",
		"my-cm",
		"creator=someone-else",
		"--overwrite",
	)
	suite.T().Log("Running command:", cmd.String())
	output, err := cmd.CombinedOutput()
	if suite.Error(err) {
		suite.Contains(string(output), "creator annotation is immutable")
	}
}

func (suite *E2ESuite) TestClusterAdmission() {
	admNamespace := tmpNamespace(suite.T())
	suite.T().Log("Creating managed resource...")
	requireRun(suite.T(),
		exec.Command(
			"kubectl",
			"apply",
			"-n", admNamespace,
			"-f", path.Join(suite.projectDir, "test", "e2e", "testdata", "admission", "cluster.yaml"),
		),
	)

	var crName string
	suite.T().Log("Waiting for webhook to become ready...")
	suite.Require().EventuallyWithT(func(t *assert.CollectT) {
		cmd := exec.Command("kubectl", "create", "-f-", "-n", admNamespace, "-o=json")
		out := new(bytes.Buffer)
		cmd.Stdout = out
		cmd.Stdin = strings.NewReader(`
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  generateName: wait-webhook-
`)
		require.NoError(t, cmd.Run())
		var createdCR rbacv1.ClusterRole
		require.NoError(t, json.Unmarshal(out.Bytes(), &createdCR))
		require.NotEmpty(t, createdCR.ObjectMeta.Annotations)
		require.NotEmpty(t, createdCR.ObjectMeta.Annotations["creator"])
		crName = createdCR.ObjectMeta.Name
	}, 1*time.Minute, 100*time.Microsecond, "Waiting for webhook to become ready and installed")

	suite.T().Log("Verifying annotation rejection...")
	cmd := exec.Command(
		"kubectl",
		"annotate",
		"clusterrole",
		crName,
		"creator=someone-else",
		"--overwrite",
	)
	suite.T().Log("Running command:", cmd.String())
	output, err := cmd.CombinedOutput()
	if suite.Error(err) {
		suite.Contains(string(output), "creator annotation is immutable")
	}
}
