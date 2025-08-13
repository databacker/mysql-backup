package test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	imagetypes "github.com/docker/docker/api/types/image"
)

func startDatabase(dc *dockerContext, baseDir, image, name string) (containerPort, error) {
	resp, err := dc.cli.ImagePull(context.Background(), image, imagetypes.PullOptions{})
	if err != nil {
		return containerPort{}, fmt.Errorf("failed to pull mysql image: %v", err)
	}
	_, _ = io.Copy(os.Stdout, resp)
	_ = resp.Close()

	// start the mysql container; configure it for lots of debug logging, in case we need it
	mysqlConf := `
[mysqld]
log_error       =/var/log/mysql/mysql_error.log
general_log_file=/var/log/mysql/mysql.log
general_log     =1
slow_query_log  =1
slow_query_log_file=/var/log/mysql/mysql_slow.log
long_query_time =2
log_queries_not_using_indexes = 1
`
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return containerPort{}, fmt.Errorf("failed to create mysql base directory: %v", err)
	}
	confFile := filepath.Join(baseDir, "log.cnf")
	if err := os.WriteFile(confFile, []byte(mysqlConf), 0644); err != nil {
		return containerPort{}, fmt.Errorf("failed to write mysql config file: %v", err)
	}
	logDir := filepath.Join(baseDir, "mysql_logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return containerPort{}, fmt.Errorf("failed to create mysql log directory: %v", err)
	}

	// start mysql
	cid, port, err := dc.startContainer(
		image, name, "3306/tcp", []string{fmt.Sprintf("%s:/etc/mysql/conf.d/log.conf:ro", confFile), fmt.Sprintf("%s:/var/log/mysql", logDir)}, nil, []string{
			fmt.Sprintf("MYSQL_ROOT_PASSWORD=%s", mysqlRootPass),
			"MYSQL_DATABASE=tester",
			fmt.Sprintf("MYSQL_USER=%s", mysqlUser),
			fmt.Sprintf("MYSQL_PASSWORD=%s", mysqlPass),
		})
	if err != nil {
		return containerPort{}, fmt.Errorf("failed to start mysql container: %v", err)
	}
	return containerPort{name: name, id: cid, port: port}, nil
}
