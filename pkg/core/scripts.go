package core

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
)

func runScripts(dir string, env map[string]string) error {
	files, err := os.ReadDir(dir)
	// if the directory does not exist, do not worry about it
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	for _, f := range files {
		// ignore directories and any files we cannot execute
		fi, err := f.Info()
		if err != nil {
			return fmt.Errorf("error getting file info %s: %v", f.Name(), err)
		}
		if f.IsDir() || fi.Mode()&0111 == 0 {
			continue
		}
		// execute the file
		envSlice := os.Environ()
		for k, v := range env {
			envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
		}
		cmd := exec.Command(path.Join(dir, f.Name()))
		cmd.Env = envSlice
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("error running file %s: %v", f.Name(), err)
		}
	}
	return nil
}

func oneScript(target string, env map[string]string) ([]byte, error) {
	// execute the file
	envSlice := os.Environ()
	for k, v := range env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
	}
	cmd := exec.Command(target)
	cmd.Env = envSlice
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("error running file %s: %v", target, err)
	}
	return stdout.Bytes(), nil
}
