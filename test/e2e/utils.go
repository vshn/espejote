//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/shlex"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
	kustomizeapi "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"
)

const (
	certmanagerVersion = "v1.19.1"
	certmanagerURLTmpl = "https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml"

	defaultKindBinary  = "kind"
	defaultKindCluster = "kind"

	prometheusOperatorVersion = "v0.86.2"
	prometheusOperatorURL     = "https://github.com/prometheus-operator/prometheus-operator/releases/download/%s/stripped-down-crds.yaml"

	// projectImage is the name of the image which will be build and loaded
	// with the code source changes to be tested.
	projectRepository = "local.dev/espejote"
	projectTag        = "latest"
	projectImage      = projectRepository + ":" + projectTag
)

// requireRun executes the provided command within this context
func requireRun(t *testing.T, cmd *exec.Cmd) string {
	t.Helper()

	t.Log("Executing command:", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%q failed with error %q", strings.Join(cmd.Args, " "), string(output))
	return string(output)
}

// uninstallCertManager uninstalls the cert manager
func uninstallCertManager(t *testing.T) {
	t.Helper()

	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	requireRun(t, exec.Command("kubectl", "delete", "-f", url))

	// Delete leftover leases in kube-system (not cleaned by default)
	kubeSystemLeases := []string{
		"cert-manager-cainjector-leader-election",
		"cert-manager-controller",
	}
	for _, lease := range kubeSystemLeases {
		requireRun(t,
			exec.Command("kubectl", "delete", "lease", lease,
				"-n", "kube-system", "--ignore-not-found", "--force", "--grace-period=0"),
		)
	}
}

func installPrometheusOperatorCRDs(t *testing.T) {
	t.Helper()

	url := fmt.Sprintf(prometheusOperatorURL, prometheusOperatorVersion)
	requireRun(t, exec.Command("kubectl", "apply", "-f", url))
}

// installCertManager installs the cert manager bundle.
func installCertManager(t *testing.T) {
	t.Helper()

	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)

	requireRun(t, exec.Command("kubectl", "apply", "-f", url))

	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	requireDeploymentAvailable(t, "cert-manager", "cert-manager-webhook")
}

// requireDeploymentAvailable waits until the specified deployment is available
func requireDeploymentAvailable(t *testing.T, namespace, name string) {
	t.Helper()

	cmd := exec.Command("kubectl", "wait", "deployment.apps", name,
		"--for", "condition=Available",
		"--namespace", namespace,
		"--timeout", "3m",
	)
	t.Log("Running command:", cmd)
	if err := cmd.Run(); err != nil {
		t.Logf("failed to wait for deployment: %s", err.Error())
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			runAndLogToT(t, exec.Command("kubectl", "describe", "deployment.apps", name,
				"--namespace", namespace,
			))
			runAndLogToT(t, exec.Command("kubectl", "describe", "pods",
				"--namespace", namespace,
			))
			runAndLogToT(t, exec.Command("kubectl", "logs", "deployment.apps/"+name,
				"--namespace", namespace,
			))
		}
		t.FailNow()
	}
}

func runAndLogToT(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()

	cmd.Stdout = t.Output()
	cmd.Stderr = t.Output()
	t.Log("Executing command:", strings.Join(cmd.Args, " "))
	return cmd.Run()
}

// isCertManagerCRDsInstalled checks if any Cert Manager CRDs are installed
// by verifying the existence of key CRDs related to Cert Manager.
func isCertManagerCRDsInstalled(t *testing.T) bool {
	t.Helper()

	// List of common Cert Manager CRDs
	certManagerCRDs := []string{
		"certificates.cert-manager.io",
		"issuers.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"certificaterequests.cert-manager.io",
		"orders.acme.cert-manager.io",
		"challenges.acme.cert-manager.io",
	}

	// Execute the kubectl command to get all CRDs
	cmd := exec.Command("kubectl", "get", "crds")
	output := requireRun(t, cmd)

	// Check if any of the Cert Manager CRDs are present
	crdList := getNonEmptyLines(output)
	for _, crd := range certManagerCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}

	return false
}

// requireLoadImageToKindClusterWithName loads a local docker image to the kind cluster
func requireLoadImageToKindClusterWithName(t *testing.T, name string) {
	t.Helper()

	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindOptions := []string{"load", "docker-image", name, "--name", cluster}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}
	// We use go tool to run kind, so we need to split the command properly
	cs, err := shlex.Split(kindBinary)
	require.NoError(t, err)

	cmd := exec.Command(cs[0], append(cs[1:], kindOptions...)...)
	cmd.Dir = getProjectDir(t)
	requireRun(t, cmd)
}

// getNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func getNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// getProjectDir will return the directory where the project is
func getProjectDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)

	return strings.ReplaceAll(wd, "/test/e2e", "")
}

// tmpNamespace creates a temporary namespace and returns its name
func tmpNamespace(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("kubectl", "create", "-f", "-", "-ojsonpath={.metadata.name}")
	cmd.Stdin = strings.NewReader(`
apiVersion: v1
kind: Namespace
metadata:
  generateName: espejote-e2e-
`)
	return strings.TrimSpace(requireRun(t, cmd))
}

func buildAndUploadManagerImage(t *testing.T, projectDir, buildDir string) {
	t.Helper()

	buildCmd := exec.Command("go", "build", "-o", path.Join(buildDir, "espejote"))
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
	buildCmd.Dir = projectDir
	requireRun(t, buildCmd)
	imageBuildCmd := exec.Command("docker", "build", "-f", path.Join(projectDir, "Dockerfile"), "-t", projectImage, buildDir)
	requireRun(t, imageBuildCmd)

	requireLoadImageToKindClusterWithName(t, projectImage)
}

func installControllerManager(t *testing.T, projectDir, tmpDir string) {
	t.Helper()

	requireFilepathRel := func(base, target string) string {
		rel, err := filepath.Rel(base, target)
		require.NoError(t, err)
		return rel
	}

	kf := kustomizeapi.Kustomization{
		Resources: []string{
			requireFilepathRel(tmpDir, path.Join(projectDir, "config", "crd")),
			requireFilepathRel(tmpDir, path.Join(projectDir, "config", "default")),
		},
		// Override the manager image to use the locally built one
		Images: []kustomizeapi.Image{
			{
				Name:    "ghcr.io/vshn/espejote",
				NewName: projectRepository,
				NewTag:  projectTag,
			},
		},
		// Replace imagePullPolicy to Never in all managed deployments to use the locally loaded image in kind
		Replacements: []kustomizeapi.ReplacementField{{
			Replacement: kustomizeapi.Replacement{
				SourceValue: ptr.To("Never"),
				Targets: []*kustomizeapi.TargetSelector{{
					Select: &kustomizeapi.Selector{
						ResId: resid.NewResIdKindOnly("Deployment", ""),
					},
					Options: &kustomizeapi.FieldOptions{Create: true},
					FieldPaths: []string{
						"spec.template.spec.containers.*.imagePullPolicy",
					},
				}},
			},
		}},
	}

	enc, err := json.Marshal(kf)
	require.NoError(t, err)
	t.Log("Generated kustomization:", string(enc))

	require.NoError(t, os.WriteFile(path.Join(tmpDir, "kustomization.yaml"), enc, 0644))

	cmd := exec.Command("kubectl", "apply", "-k", tmpDir)
	requireRun(t, cmd)
}
