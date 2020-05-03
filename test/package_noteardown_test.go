//go:build integration && keepcontainers

package test

import "fmt"

func teardown(dc *dockerContext, cids ...string) error {
	return nil
}
