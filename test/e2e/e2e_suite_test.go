//go:build e2e
// +build e2e

package e2e

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/suite"
)

var (
	// Optional Environment Variables:
	// - CERT_MANAGER_INSTALL_SKIP=true: Skips CertManager installation during test setup.
	// These variables are useful if CertManager is already installed, avoiding
	// re-installation and conflicts.
	skipCertManagerInstall = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
)

type E2ESuite struct {
	suite.Suite

	projectDir string
	buildDir   string

	// isCertManagerAlreadyInstalled will be set true when CertManager CRDs be found on the cluster
	isCertManagerAlreadyInstalled bool
}

func (suite *E2ESuite) SetupSuite() {
	suite.projectDir = getProjectDir(suite.T())
	buildDir, err := os.MkdirTemp(suite.projectDir, "espejote-e2e-")
	suite.Require().NoError(err)
	suite.buildDir = buildDir
	suite.T().Logf("Setting up E2E suite... projectDir=%s, buildDir=%s", suite.projectDir, suite.buildDir)

	suite.isCertManagerAlreadyInstalled = isCertManagerCRDsInstalled(suite.T())
	if !skipCertManagerInstall && !suite.isCertManagerAlreadyInstalled {
		installCertManager(suite.T())
	}
	installPrometheusOperatorCRDs(suite.T())

	suite.T().Log("Building and uploading manager image to kind cluster...")
	buildAndUploadManagerImage(suite.T(), suite.projectDir, suite.buildDir)

	suite.T().Log("Installing controller manager...")
	installControllerManager(suite.T(), suite.projectDir, suite.buildDir)
	requireDeploymentAvailable(suite.T(), "espejote-system", "espejote-controller-manager")
}

func (suite *E2ESuite) TearDownSuite() {
	suite.T().Log("Tearing down E2E suite...")
	if !skipCertManagerInstall && !suite.isCertManagerAlreadyInstalled {
		suite.T().Log("Uninstalling CertManager...")
		uninstallCertManager(suite.T())
	}

	suite.T().Log("Removing build directory:", suite.buildDir)
	suite.Require().NoError(os.RemoveAll(suite.buildDir))
}

func (suite *E2ESuite) TearDownTest() {
	if suite.T().Failed() {
		suite.T().Log("Test failed, fetching controller manager logs...")
		runAndLogToT(suite.T(), exec.Command("kubectl", "logs", "deployment.apps/espejote-controller-manager",
			"--namespace", "espejote-system",
		))
	}
}

// TestE2E runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purpose of being used in CI jobs.
// The default setup requires Kind, builds/loads the Manager Docker image locally, installs CertManager,
// and Prometheus Operator CRDs.
func TestE2E(t *testing.T) {
	suite.Run(t, new(E2ESuite))
}
