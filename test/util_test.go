package test

import (
	"os"
	"testing"
)

const (
	integrationTestEnvVar = "TEST_INTEGRATION"
)

func IsIntegration() bool {
	val, isIntegration := os.LookupEnv(integrationTestEnvVar)
	return isIntegration && val != "false" && val != ""
}

func CheckSkipIntegration(t *testing.T, name string) {
	if !IsIntegration() {
		t.Skipf("Skipping integration test %s, set %s to run", integrationTestEnvVar, name)
	}
}
