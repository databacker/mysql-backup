//go:build integration

package test

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/storage/credentials"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/moby/moby/pkg/archive"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

const (
	mysqlUser     = "user"
	mysqlPass     = "abcdefg"
	mysqlRootUser = "root"
	mysqlRootPass = "root"
	smbImage      = "mysqlbackup_smb_test:latest"
	mysqlImage    = "mysql:8.2.0"
	bucketName    = "mybucket"
)

// original filters also filtered out anything that started with "/\*![\d]{5}.\*/;$",
// i.e. in a comment, ending in ;, and a 5 digit number in the comment at the beginning
// after a !
// not sure we want to filter those out
var dumpFilterRegex = []*regexp.Regexp{
	//regexp.MustCompile("^.*SET character_set_client.*|s/^\/\*![0-9]\{5\}.*\/;$"),
	regexp.MustCompile(`(?i)^\s*-- MySQL dump .*$`),
	regexp.MustCompile(`(?i)^\s*-- Go SQL dump .*$`),
	regexp.MustCompile(`(?i)^\s*-- Dump completed on .*`),
}

type containerPort struct {
	name string
	id   string
	port int
}
type dockerContext struct {
	cli *client.Client
}

type backupTarget struct {
	s         string
	id        string
	subid     string
	localPath string
}

type testOptions struct {
	compact      bool
	targets      []string
	dc           *dockerContext
	base         string
	prePost      bool
	backupData   []byte
	mysql        containerPort
	smb          containerPort
	s3           string
	s3backend    gofakes3.Backend
	checkCommand checkCommand
	dumpOptions  core.DumpOptions
}

func (t backupTarget) String() string {
	return t.s
}
func (t backupTarget) WithPrefix(prefix string) string {
	// prepend the prefix to the path, but only to the path
	// and only if it is file scheme
	scheme := t.Scheme()
	if scheme != "file" && scheme != "" {
		return t.s
	}
	u, err := url.Parse(t.s)
	if err != nil {
		return ""
	}
	u.Path = filepath.Join(prefix, u.Path)
	return u.String()
}

func (t backupTarget) Scheme() string {
	u, err := url.Parse(t.s)
	if err != nil {
		return ""
	}
	return u.Scheme
}
func (t backupTarget) Host() string {
	u, err := url.Parse(t.s)
	if err != nil {
		return ""
	}
	return u.Host
}
func (t backupTarget) Path() string {
	u, err := url.Parse(t.s)
	if err != nil {
		return ""
	}
	return u.Path
}

// uniquely generated ID of the target. Shared across multiple targets that are part of the same
// backup set, e.g. "file:///backups/ smb://smb/path", where each sub has its own subid
func (t backupTarget) ID() string {
	return t.id
}
func (t backupTarget) SubID() string {
	return t.subid
}

func (t backupTarget) LocalPath() string {
	return t.localPath
}

// getDockerContext retrieves a Docker context with a prepared client handle
func getDockerContext() (*dockerContext, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &dockerContext{cli}, nil
}

func (d *dockerContext) execInContainer(ctx context.Context, cid string, cmd []string) (types.HijackedResponse, int, error) {
	execConfig := types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}
	execResp, err := d.cli.ContainerExecCreate(ctx, cid, execConfig)
	if err != nil {
		return types.HijackedResponse{}, 0, fmt.Errorf("failed to create exec: %w", err)
	}
	var execStartCheck types.ExecStartCheck
	attachResp, err := d.cli.ContainerExecAttach(ctx, execResp.ID, execStartCheck)
	if err != nil {
		return attachResp, 0, fmt.Errorf("failed to attach to exec: %w", err)
	}
	var (
		retryMax   = 20
		retrySleep = 1
		success    bool
		inspect    types.ContainerExecInspect
	)
	for i := 0; i < retryMax; i++ {
		inspect, err = d.cli.ContainerExecInspect(ctx, execResp.ID)
		if err != nil {
			return attachResp, 0, fmt.Errorf("failed to inspect exec: %w", err)
		}
		if !inspect.Running {
			success = true
			break
		}
		time.Sleep(time.Duration(retrySleep) * time.Second)
	}
	if !success {
		return attachResp, 0, fmt.Errorf("failed to wait for exec to finish")
	}
	return attachResp, inspect.ExitCode, nil
}
func (d *dockerContext) waitForDBConnectionAndGrantPrivileges(mysqlCID, dbuser, dbpass string) error {
	ctx := context.Background()

	// Allow up to 20 seconds for the mysql database to be ready
	retryMax := 20
	retrySleep := 1
	success := false

	for i := 0; i < retryMax; i++ {
		// Check database connectivity
		dbValidate := []string{"mysql", fmt.Sprintf("-u%s", dbuser), fmt.Sprintf("-p%s", dbpass), "--protocol=tcp", "-h127.0.0.1", "--wait", "--connect_timeout=20", "tester", "-e", "select 1;"}
		attachResp, exitCode, err := d.execInContainer(ctx, mysqlCID, dbValidate)
		if err != nil {
			return fmt.Errorf("failed to attach to exec: %w", err)
		}
		defer attachResp.Close()
		if exitCode == 0 {
			success = true
			break
		}

		time.Sleep(time.Duration(retrySleep) * time.Second)
	}

	if !success {
		return fmt.Errorf("failed to connect to database after %d tries", retryMax)
	}

	// Ensure the user has the right privileges
	dbGrant := []string{"mysql", fmt.Sprintf("-u%s", dbpass), fmt.Sprintf("-p%s", dbpass), "--protocol=tcp", "-h127.0.0.1", "-e", "grant process on *.* to user;"}
	attachResp, exitCode, err := d.execInContainer(ctx, mysqlCID, dbGrant)
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()
	var bufo, bufe bytes.Buffer
	_, _ = stdcopy.StdCopy(&bufo, &bufe, attachResp.Reader)
	if exitCode != 0 {
		return fmt.Errorf("failed to grant privileges to user: %s", bufe.String())
	}

	return nil
}

func (d *dockerContext) startSMBContainer(image, name, base string) (cid string, port int, err error) {
	return d.startContainer(image, name, "445/tcp", []string{fmt.Sprintf("%s:/share/backups", base)}, nil, nil)
}

func (d *dockerContext) startContainer(image, name, portMap string, binds []string, cmd []string, env []string) (cid string, port int, err error) {
	ctx := context.Background()

	// Start the SMB container
	containerConfig := &container.Config{
		Image: image,
		Cmd:   cmd,
		Labels: map[string]string{
			"mysqltest": "",
		},
		Env: env,
	}
	hostConfig := &container.HostConfig{
		Binds: binds,
	}
	var containerPort nat.Port
	if portMap != "" {
		containerPort = nat.Port(portMap)
		containerConfig.ExposedPorts = nat.PortSet{
			containerPort: struct{}{},
		}
		hostConfig.PortBindings = nat.PortMap{
			containerPort: []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: ""}},
		}
	}
	resp, err := d.cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, name)
	if err != nil {
		return
	}
	cid = resp.ID
	err = d.cli.ContainerStart(ctx, cid, container.StartOptions{})
	if err != nil {
		return
	}

	// Retrieve the randomly assigned port
	if portMap == "" {
		return
	}
	var (
		maxRetries = 3
		delay      = 500 * time.Millisecond
		inspect    types.ContainerJSON
	)
	for i := 0; i < maxRetries; i++ {
		// Inspect the container
		inspect, err = d.cli.ContainerInspect(ctx, cid)
		if err != nil {
			return
		}

		// Check the desired status
		if inspect.State.Running && len(inspect.NetworkSettings.Ports[containerPort]) > 0 {
			break
		}

		// Wait for delay ms before trying again
		time.Sleep(delay)
	}

	if len(inspect.NetworkSettings.Ports[containerPort]) == 0 {
		err = fmt.Errorf("no port mapping found for container %s %s port %s", cid, name, containerPort)
		return
	}
	portStr := inspect.NetworkSettings.Ports[containerPort][0].HostPort
	port, err = strconv.Atoi(portStr)

	return
}

func (d *dockerContext) makeSMB(smbImage string) error {
	ctx := context.Background()

	// Build the smbImage
	buildSMBImageOpts := types.ImageBuildOptions{
		Context: nil,
		Tags:    []string{smbImage},
		Remove:  true,
	}

	tar, err := archive.TarWithOptions("ctr/", &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("failed to create tar archive: %w", err)
	}
	buildSMBImageOpts.Context = io.NopCloser(tar)

	resp, err := d.cli.ImageBuild(ctx, buildSMBImageOpts.Context, buildSMBImageOpts)
	if err != nil {
		return fmt.Errorf("failed to build smb image: %w", err)
	}
	io.Copy(os.Stdout, resp.Body)
	resp.Body.Close()

	return nil
}

func (d *dockerContext) createBackupFile(mysqlCID, mysqlUser, mysqlPass, outfile, compactOutfile string) error {
	ctx := context.Background()

	// Create and populate the table
	mysqlCreateCmd := []string{"mysql", "-hlocalhost", "--protocol=tcp", fmt.Sprintf("-u%s", mysqlUser), fmt.Sprintf("-p%s", mysqlPass), "-e", `
	use tester;
	create table t1
	(id int, name varchar(20), j json, d date, t time, dt datetime, ts timestamp);
	INSERT INTO t1 (id,name,j,d,t,dt,ts)
	VALUES
	(1, "John", '{"a":"b"}', "2012-11-01", "00:15:00", "2012-11-01 00:15:00", "2012-11-01 00:15:00"),
	(2, "Jill", '{"c":true}', "2012-11-02", "00:16:00", "2012-11-02 00:16:00", "2012-11-02 00:16:00"),
	(3, "Sam", '{"d":24}', "2012-11-03", "00:17:00", "2012-11-03 00:17:00", "2012-11-03 00:17:00"),
	(4, "Sarah", '{"a":"b"}', "2012-11-04", "00:18:00", "2012-11-04 00:18:00", "2012-11-04 00:18:00");
	create view view1 as select id, name from t1;
	`}
	attachResp, exitCode, err := d.execInContainer(ctx, mysqlCID, mysqlCreateCmd)
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()
	var bufo, bufe bytes.Buffer
	_, _ = stdcopy.StdCopy(&bufo, &bufe, attachResp.Reader)
	if exitCode != 0 {
		return fmt.Errorf("failed to create table: %s", bufe.String())
	}

	// Dump the database - do both compact and non-compact
	mysqlDumpCompactCmd := []string{"mysqldump", "-hlocalhost", "--protocol=tcp", "--complete-insert", fmt.Sprintf("-u%s", mysqlUser), fmt.Sprintf("-p%s", mysqlPass), "--compact", "--databases", "tester"}
	attachResp, exitCode, err = d.execInContainer(ctx, mysqlCID, mysqlDumpCompactCmd)
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()
	if exitCode != 0 {
		return fmt.Errorf("failed to dump database: %w", err)
	}

	fCompact, err := os.Create(compactOutfile)
	if err != nil {
		return err
	}
	defer fCompact.Close()

	_, _ = stdcopy.StdCopy(fCompact, &bufe, attachResp.Reader)

	bufo.Reset()
	bufe.Reset()

	mysqlDumpCmd := []string{"mysqldump", "-hlocalhost", "--protocol=tcp", "--complete-insert", fmt.Sprintf("-u%s", mysqlUser), fmt.Sprintf("-p%s", mysqlPass), "--databases", "tester"}
	attachResp, exitCode, err = d.execInContainer(ctx, mysqlCID, mysqlDumpCmd)
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()
	if exitCode != 0 {
		return fmt.Errorf("failed to dump database: %w", err)
	}

	f, err := os.Create(outfile)
	if err != nil {
		return err
	}
	defer f.Close()

	_, _ = stdcopy.StdCopy(f, &bufe, attachResp.Reader)
	return err
}

func (d *dockerContext) logContainers(cids ...string) error {
	ctx := context.Background()
	for _, cid := range cids {
		logOptions := container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
		}
		logs, err := d.cli.ContainerLogs(ctx, cid, logOptions)
		if err != nil {
			return fmt.Errorf("failed to get logs for container %s: %w", cid, err)
		}
		defer logs.Close()

		if _, err := io.Copy(os.Stdout, logs); err != nil {
			return fmt.Errorf("failed to stream logs for container %s: %w", cid, err)
		}
	}
	return nil
}

func (d *dockerContext) rmContainers(cids ...string) error {
	ctx := context.Background()
	for _, cid := range cids {
		if err := d.cli.ContainerKill(ctx, cid, "SIGKILL"); err != nil {
			return fmt.Errorf("failed to kill container %s: %w", cid, err)
		}

		rmOpts := container.RemoveOptions{
			RemoveVolumes: true,
			Force:         true,
		}
		if err := d.cli.ContainerRemove(ctx, cid, rmOpts); err != nil {
			return fmt.Errorf("failed to remove container %s: %w", cid, err)
		}
	}
	return nil
}

// we need to run through each each target and test the backup.
// before the first run, we:
// - start the sql database
// - populate it with a few inserts/creates
// - run a single clear backup
// for each stage, we:
// - clear the target
// - run the backup
// - check that the backup now is there in the right format
// - clear the target

func setup(dc *dockerContext, base, backupFile, compactBackupFile string) (mysql, smb containerPort, s3url string, s3backend gofakes3.Backend, err error) {
	if err := dc.makeSMB(smbImage); err != nil {
		return mysql, smb, s3url, s3backend, fmt.Errorf("failed to build smb image: %v", err)
	}

	// start up the various containers
	smbCID, smbPort, err := dc.startSMBContainer(smbImage, "smb", base)
	if err != nil {
		return mysql, smb, s3url, s3backend, fmt.Errorf("failed to start smb container: %v", err)
	}
	smb = containerPort{name: "smb", id: smbCID, port: smbPort}

	// start the s3 container
	s3backend = s3mem.New()
	// create the bucket we will use for tests
	if err := s3backend.CreateBucket(bucketName); err != nil {
		return mysql, smb, s3url, s3backend, fmt.Errorf("failed to create bucket: %v", err)
	}
	s3 := gofakes3.New(s3backend)
	s3server := httptest.NewServer(s3.Server())
	s3url = s3server.URL

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
	confFile := filepath.Join(base, "log.cnf")
	if err := os.WriteFile(confFile, []byte(mysqlConf), 0644); err != nil {
		return mysql, smb, s3url, s3backend, fmt.Errorf("failed to write mysql config file: %v", err)
	}
	logDir := filepath.Join(base, "mysql_logs")
	if err := os.Mkdir(logDir, 0755); err != nil {
		return mysql, smb, s3url, s3backend, fmt.Errorf("failed to create mysql log directory: %v", err)
	}
	// ensure we have mysql image
	resp, err := dc.cli.ImagePull(context.Background(), mysqlImage, types.ImagePullOptions{})
	if err != nil {
		return mysql, smb, s3url, s3backend, fmt.Errorf("failed to pull mysql image: %v", err)
	}
	io.Copy(os.Stdout, resp)
	resp.Close()
	mysqlCID, mysqlPort, err := dc.startContainer(mysqlImage, "mysql", "3306/tcp", []string{fmt.Sprintf("%s:/etc/mysql/conf.d/log.conf:ro", confFile), fmt.Sprintf("%s:/var/log/mysql", logDir)}, nil, []string{
		fmt.Sprintf("MYSQL_ROOT_PASSWORD=%s", mysqlRootPass),
		"MYSQL_DATABASE=tester",
		fmt.Sprintf("MYSQL_USER=%s", mysqlUser),
		fmt.Sprintf("MYSQL_PASSWORD=%s", mysqlPass),
	})
	if err != nil {
		return
	}
	mysql = containerPort{name: "mysql", id: mysqlCID, port: mysqlPort}

	if err = dc.waitForDBConnectionAndGrantPrivileges(mysqlCID, mysqlRootUser, mysqlRootPass); err != nil {
		return
	}

	// create the backup file
	log.Debugf("Creating backup file")
	if err := dc.createBackupFile(mysql.id, mysqlUser, mysqlPass, backupFile, compactBackupFile); err != nil {
		return mysql, smb, s3url, s3backend, fmt.Errorf("failed to create backup file: %v", err)
	}
	return
}

// backupTargetsToStorage convert a list of backupTarget to a list of core.Storage
func backupTargetsToStorage(targets []backupTarget, base, s3 string) ([]storage.Storage, error) {
	var targetVals []storage.Storage
	// all targets should have the same sequence, with varying subsequence, so take any one
	for _, tgt := range targets {
		tg := tgt.String()
		tg = tgt.WithPrefix(base)
		localPath := tgt.LocalPath()
		if err := os.MkdirAll(localPath, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create local path %s: %v", localPath, err)
		}
		store, err := storage.ParseURL(tg, credentials.Creds{AWS: credentials.AWSCreds{Endpoint: s3}})
		if err != nil {
			return nil, fmt.Errorf("invalid target url: %v", err)
		}
		targetVals = append(targetVals, store)

	}
	return targetVals, nil
}

// targetToTargets take a target string, which can contain multiple target URLs, and convert it to a list of backupTarget
func targetToTargets(target string, sequence int, smb containerPort, base string) ([]backupTarget, error) {
	var (
		targets    = strings.Fields(target)
		allTargets []backupTarget
	)
	id := fmt.Sprintf("%05d", rand.Intn(10000))
	for i, t := range targets {
		subid := fmt.Sprintf("%02d", i)
		// parse the URL, taking any smb protocol and replacing the host:port with our local host:port
		u, err := url.Parse(t)
		if err != nil {
			return nil, err
		}

		// localPath tracks the local path that is equivalent to where the backup
		// target points.
		var localPath string
		relativePath := filepath.Join(id, subid, "data")
		subPath := filepath.Join("/backups", relativePath)
		localPath = filepath.Join(base, subPath)

		switch u.Scheme {
		case "smb":
			u.Host = fmt.Sprintf("localhost:%d", smb.port)
			u.Path = filepath.Join(u.Path, subPath)
		case "file", "":
			// explicit or implicit file
			u.Scheme = "file"
			u.Path = subPath
		case "s3":
			// prepend the bucket name to the path
			// because fakes3 uses path-style naming
			u.Path = filepath.Join(u.Hostname(), subPath)
		default:
		}
		// we ignore the path, instead sending the backup to a unique directory
		// this WILL break when we do targets that are smb or the like.
		// will need to find a way to fix this.
		finalTarget := u.String()
		allTargets = append(allTargets, backupTarget{s: finalTarget, id: id, subid: subid, localPath: localPath})
	}
	// Configure the container
	if len(allTargets) == 0 {
		return nil, errors.New("must provide at least one target")
	}
	return allTargets, nil
}

type checkCommand func(t *testing.T, base string, validBackup []byte, s3backend gofakes3.Backend, targets []backupTarget, results core.DumpResults)

func runTest(t *testing.T, opts testOptions) {
	// run backups for each target
	for i, target := range opts.targets {
		t.Run(target, func(t *testing.T) {
			// should add t.Parallel() here for parallel execution, but later
			log.Debugf("Running test for target '%s'", target)
			allTargets, err := targetToTargets(target, i, opts.smb, opts.base)
			if err != nil {
				t.Fatalf("failed to parse target: %v", err)
			}
			log.Debugf("Populating data for target %s", target)
			if err := populateVol(opts.base, allTargets); err != nil {
				t.Fatalf("failed to populate volume for target %s: %v", target, err)
			}
			if err := populatePrePost(opts.base, allTargets); err != nil {
				t.Fatalf("failed to populate pre-post for target %s: %v", target, err)
			}
			log.Debugf("Running backup for target %s", target)
			opts.dumpOptions.DBConn = database.Connection{
				User: mysqlUser,
				Pass: mysqlPass,
				Host: "localhost",
				Port: opts.mysql.port,
			}
			// take []backupTarget and convert to []storage.Storage that can be passed to DumpOptions
			targetVals, err := backupTargetsToStorage(allTargets, opts.base, opts.s3)
			if err != nil {
				t.Fatalf("failed to convert backup targets to storage: %v", err)
			}

			id := allTargets[0].ID()

			opts.dumpOptions.Targets = targetVals
			opts.dumpOptions.PreBackupScripts = filepath.Join(opts.base, "backups", id, "pre-backup")
			opts.dumpOptions.PostBackupScripts = filepath.Join(opts.base, "backups", id, "post-backup")

			timerOpts := core.TimerOptions{
				Once: true,
			}
			executor := &core.Executor{}
			executor.SetLogger(log.New())

			var results core.DumpResults
			if err := executor.Timer(timerOpts, func() error {
				ctx := context.Background()
				ret, err := executor.Dump(ctx, opts.dumpOptions)
				results = ret
				return err
			}); err != nil {
				t.Fatalf("failed to run dump test: %v", err)
			}

			// check that the filename matches the pattern
			for i, upload := range results.Uploads {
				expected, err := core.ProcessFilenamePattern(opts.dumpOptions.FilenamePattern, results.Time, results.Timestamp, opts.dumpOptions.Compressor.Extension())
				if err != nil {
					t.Fatalf("failed to process filename pattern: %v", err)
				}
				clean := opts.dumpOptions.Targets[i].Clean(expected)
				if upload.Filename != clean {
					t.Fatalf("filename %s does not match expected %s", upload.Filename, clean)
				}
			}

			opts.checkCommand(t, opts.base, opts.backupData, opts.s3backend, allTargets, results)
		})
	}
}

func checkDumpTest(t *testing.T, base string, expected []byte, s3backend gofakes3.Backend, targets []backupTarget, results core.DumpResults) {
	// all of it is in the volume we created, so check from there
	var (
		backupDataReader io.Reader
	)
	// we might have multiple targets
	for i, target := range targets {
		// check that the expected backups are in the right place
		var (
			id                = target.ID()
			scheme            = target.Scheme()
			postBackupOutFile = fmt.Sprintf("%s/backups/%s/post-backup/post-backup.txt", base, id)
			preBackupOutFile  = fmt.Sprintf("%s/backups/%s/pre-backup/pre-backup.txt", base, id)
			// useful for restore tests, which are disabled for now, so commented out
			//postRestoreFile   = fmt.Sprintf("%s/backups/%s/post-restore/post-restore.txt", base, sequence)
			//preRestoreFile    = fmt.Sprintf("%s/backups/%s/pre-restore/pre-restore.txt", base, sequence)
		)
		// postBackup and preBackup are only once for a set of targets
		if i == 0 {
			msg := fmt.Sprintf("%s %s post-backup", id, target.String())
			if _, err := os.Stat(postBackupOutFile); err != nil {
				t.Errorf("%s script didn't run, output file doesn't exist", msg)
			}
			os.RemoveAll(postBackupOutFile)

			msg = fmt.Sprintf("%s %s pre-backup", id, target.String())
			if _, err := os.Stat(preBackupOutFile); err != nil {
				t.Errorf("%s script didn't run, output file doesn't exist", msg)
			}
			os.RemoveAll(preBackupOutFile)
		}
		p := target.Path()
		if p == "" {
			t.Fatalf("target %s has no path", target.String())
			return
		}

		targetFilename := results.Uploads[i].Filename

		switch scheme {
		case "s3":
			// because we had to add the bucket at the beginning of the path, because fakes3
			// does path-style, remove it now
			// the object is sensitive to not starting with '/'
			// we do it in 2 steps, though, so that if it was not already starting with a `/`,
			// we still will remove the bucketName
			p = strings.TrimPrefix(p, "/")
			p = strings.TrimPrefix(p, bucketName+"/")
			p = filepath.Join(p, targetFilename)
			obj, err := s3backend.GetObject(bucketName, p, nil)
			if err != nil {
				t.Fatalf("failed to get backup object %s from s3: %v", p, err)
				return
			}
			backupDataReader = obj.Contents
		default:
			var err error
			bdir := target.LocalPath()
			backupFile := filepath.Join(bdir, targetFilename)
			backupDataReader, err = os.Open(backupFile)
			if err != nil {
				t.Fatalf("failed to read backup file %s: %v", backupFile, err)
				return
			}
		}

		// extract the actual data, but filter out lines we do not care about
		b, err := gunzipUntarScanFilter(backupDataReader)
		assert.NoError(t, err, "failed to extract backup data for %s", id)
		expectedFiltered := string(filterLines(bytes.NewReader(expected)))

		// this does not work because of information like the header that is unique
		// to each format
		assert.Equal(t, expectedFiltered, string(b), "%s tar contents do not match actual dump", id)
	}

	return
}

// gunzipUntarScanFilter is a helper function to extract the actual data from a backup
// It unzips, untars getting the first file, and then scans the file for lines we do not
// care about, returning the remaining content.
func gunzipUntarScanFilter(r io.Reader) (b []byte, err error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	if _, err := tr.Next(); err != nil {
		return nil, err
	}
	return filterLines(tr), nil
}

// filterLines filters out lines that are allowed to differ
func filterLines(r io.Reader) (b []byte) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var use = true
		line := scanner.Text()
		for _, filter := range dumpFilterRegex {
			if filter.Match([]byte(line)) {
				use = false
				break
			}
		}
		if !use {
			continue
		}
		line += "\n"
		b = append(b, line...)
	}
	return b
}

func populateVol(base string, targets []backupTarget) (err error) {
	for _, target := range targets {
		dataDir := target.LocalPath()
		if err := os.MkdirAll(dataDir, 0777); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dataDir, "list"), []byte(fmt.Sprintf("target: %s\n", target)), 0666); err != nil {
			return err
		}
	}
	return
}

func populatePrePost(base string, targets []backupTarget) (err error) {
	// Create a test script for the post backup processing test
	if len(targets) == 0 {
		return fmt.Errorf("no targets specified")
	}
	id := targets[0].ID()
	workdir := filepath.Join(base, "backups", id)
	for _, dir := range []string{"pre-backup", "post-backup", "pre-restore", "post-restore"} {
		if err := os.MkdirAll(filepath.Join(workdir, dir), 0777); err != nil {
			return err
		}
		if err := os.WriteFile(
			filepath.Join(workdir, dir, "test.sh"),
			[]byte(fmt.Sprintf("#!/bin/bash\ntouch %s.txt", filepath.Join(workdir, dir, dir))),
			0777); err != nil {
			return err
		}
		// test.sh files need to be executable, but we already set them
		// might need to do this later
		// chmod -R 0777 /backups/${sequence}
		// chmod 755 /backups/${sequence}/*/test.sh
	}

	return nil
}

func TestIntegration(t *testing.T) {
	syscall.Umask(0)
	t.Run("dump", func(t *testing.T) {
		var (
			err        error
			smb, mysql containerPort
			s3         string
			s3backend  gofakes3.Backend
		)
		// temporary working directory
		base, err := os.MkdirTemp("", "backup-test-")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		// ensure that the container has full access to it
		if err := os.Chmod(base, 0o777); err != nil {
			t.Fatalf("failed to chmod temp dir: %v", err)
		}
		dc, err := getDockerContext()
		if err != nil {
			t.Fatalf("failed to get docker client: %v", err)
		}
		backupFile := filepath.Join(base, "backup.sql")
		compactBackupFile := filepath.Join(base, "backup-compact.sql")
		if mysql, smb, s3, s3backend, err = setup(dc, base, backupFile, compactBackupFile); err != nil {
			t.Fatalf("failed to setup test: %v", err)
		}
		backupData, err := os.ReadFile(backupFile)
		if err != nil {
			t.Fatalf("failed to read backup file %s: %v", backupFile, err)
		}
		compactBackupData, err := os.ReadFile(compactBackupFile)
		if err != nil {
			t.Fatalf("failed to read compact backup file %s: %v", compactBackupFile, err)
		}
		defer func() {
			// log the results before tearing down, if requested
			if err := logContainers(dc, smb.id, mysql.id); err != nil {
				log.Errorf("failed to get logs from service containers: %v", err)
			}

			// tear everything down
			if err := teardown(dc, smb.id, mysql.id); err != nil {
				log.Errorf("failed to teardown test: %v", err)
			}
		}()

		// check just the contents of a compact backup
		t.Run("full", func(t *testing.T) {
			dumpOpts := core.DumpOptions{
				Compressor: &compression.GzipCompressor{},
				Compact:    false,
			}
			runTest(t, testOptions{
				targets:      []string{"/full-backups/"},
				dc:           dc,
				base:         base,
				backupData:   backupData,
				mysql:        mysql,
				smb:          smb,
				s3:           s3,
				s3backend:    s3backend,
				dumpOptions:  dumpOpts,
				checkCommand: checkDumpTest,
			})
		})

		// check just the contents of a backup without minimizing metadata (i.e. non-compact)
		t.Run("compact", func(t *testing.T) {
			dumpOpts := core.DumpOptions{
				Compressor: &compression.GzipCompressor{},
				Compact:    true,
			}
			runTest(t, testOptions{
				targets:      []string{"/compact-backups/"},
				dc:           dc,
				base:         base,
				backupData:   compactBackupData,
				mysql:        mysql,
				smb:          smb,
				s3:           s3,
				s3backend:    s3backend,
				dumpOptions:  dumpOpts,
				checkCommand: checkDumpTest,
			})
		})

		t.Run("pattern", func(t *testing.T) {
			dumpOpts := core.DumpOptions{
				Compressor:      &compression.GzipCompressor{},
				Compact:         false,
				FilenamePattern: "backup-{{ .Sequence }}-{{ .Subsequence }}.tgz",
			}
			runTest(t, testOptions{
				targets:      []string{"/full-backups/"},
				dc:           dc,
				base:         base,
				backupData:   backupData,
				mysql:        mysql,
				smb:          smb,
				s3:           s3,
				s3backend:    s3backend,
				dumpOptions:  dumpOpts,
				checkCommand: checkDumpTest,
			})
		})

		// test targets
		t.Run("targets", func(t *testing.T) {
			// set a default region
			if err := os.Setenv("AWS_REGION", "us-east-1"); err != nil {
				t.Fatalf("failed to set AWS_REGION: %v", err)
			}
			if err := os.Setenv("AWS_ACCESS_KEY_ID", "abcdefg"); err != nil {
				t.Fatalf("failed to set AWS_ACCESS_KEY_ID: %v", err)
			}
			if err := os.Setenv("AWS_SECRET_ACCESS_KEY", "1234567"); err != nil {
				t.Fatalf("failed to set AWS_SECRET_ACCESS_KEY: %v", err)
			}
			dumpOpts := core.DumpOptions{
				Compressor: &compression.GzipCompressor{},
				Compact:    false,
			}
			runTest(t, testOptions{
				targets: []string{
					"/backups/",
					"file:///backups/",
					"smb://smb/noauth/",
					"smb://user:pass@smb/auth",
					"smb://CONF;user:pass@smb/auth",
					fmt.Sprintf("s3://%s/", bucketName),
					"file:///backups/ file:///backups/",
				},
				dc:           dc,
				base:         base,
				backupData:   backupData,
				mysql:        mysql,
				smb:          smb,
				s3:           s3,
				s3backend:    s3backend,
				dumpOptions:  dumpOpts,
				checkCommand: checkDumpTest,
			})
		})
	})
}
