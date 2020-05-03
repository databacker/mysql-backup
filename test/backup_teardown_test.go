//go:build integration

package test

import "fmt"

func teardown(dc *dockerContext, cids ...string) error {
	if err := dc.rmContainers(cids...); err != nil {
		return fmt.Errorf("failed to remove containers: %v", err)
	}
	return nil
}
