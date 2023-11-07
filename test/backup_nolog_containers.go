//go:build integration && logs

package test

func logContainers(dc *dockerContext, cids ...string) error {
	return dc.logContainers(cids...)
}
