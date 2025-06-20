//go:build keepcontainers

package test

func teardown(dc *dockerContext, cids ...string) error {
	return nil
}
