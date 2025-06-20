//go:build logs

package test

func logContainers(dc *dockerContext, cids ...string) error {
	return dc.logContainers(cids...)
}
