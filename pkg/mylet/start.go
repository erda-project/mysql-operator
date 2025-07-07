package mylet

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	v1 "github.com/erda-project/mysql-operator/api/v1"
	log "github.com/sirupsen/logrus"
	"k8s.io/utils/pointer"
)

const MaxStartup = 720 // 1h

func (mylet *Mylet) GetMysqldVersion(id int) (v string, err error) {
	dsn := fmt.Sprintf("%s:%s%d@tcp(localhost:%d)/mysql",
		mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, id, mylet.Mysql.Spec.Port)
	db, err := Open(dsn)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
		defer cancel()

		err = db.QueryRowContext(ctx, "SELECT VERSION();").Scan(&v)
	}
	return
}

func (mylet *Mylet) CheckVersion(v string) error {
	i := IndexNotDigit(v)
	if i < 1 || v[i] != '.' {
		return fmt.Errorf("mysqld major version invalid: %s", v)
	}

	majorVersion, err := strconv.Atoi(v[:i])
	if err != nil || majorVersion != mylet.Mysql.Status.Version.Major {
		return fmt.Errorf("mysqld major version not equal %d: %s", mylet.Mysql.Status.Version.Major, v)
	}
	v = v[i+1:]

	i = IndexNotDigit(v)
	if i < 1 || v[i] != '.' {
		return fmt.Errorf("mysqld minor version invalid: %s", v)
	}

	minorVersion, err := strconv.Atoi(v[:i])
	if err != nil || minorVersion != mylet.Mysql.Status.Version.Minor {
		return fmt.Errorf("mysqld minor version not equal %d: %s", mylet.Mysql.Status.Version.Minor, v)
	}
	v = v[i+1:]

	i = IndexNotDigit(v)
	if i != -1 {
		v = v[:i]
	}
	patchVersion, err := strconv.Atoi(v)
	if err != nil || patchVersion < mylet.Mysql.Status.Version.Patch {
		return fmt.Errorf("mysqld patch version must equal or greater than %d: %s", mylet.Mysql.Status.Version.Patch, v)
	}

	return nil
}

func IndexNotDigit(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return i
		}
	}
	return -1
}

func (mylet *Mylet) cleanupStalePidAndSocket() error {
	datadir := mylet.DataDir()
	patterns := []string{
		filepath.Join(datadir, "*.pid"),
		filepath.Join(datadir, "*.sock"),
		filepath.Join(datadir, "*.sock.lock"),
	}
	for _, pattern := range patterns {
		files, _ := filepath.Glob(pattern)
		for _, f := range files {
			err := os.Remove(f)
			if err == nil {
				log.Infof("清理残留文件: %s", f)
			}
		}
	}
	return nil
}

func (mylet *Mylet) Start() error {
	mylet.Lock()
	if mylet.Running {
		mylet.Unlock()
		return fmt.Errorf("already running")
	}
	mylet.Running = true
	mylet.Unlock()

	defer func() {
		mylet.Lock()
		mylet.Running = false
		mylet.Unlock()
	}()

	for {
		select {
		case <-mylet.ExitChan:
			log.Info("mylet start loop exits")
			return nil
		default:
		}

		if mylet.restartCount >= mylet.RestartLimit {
			err := fmt.Errorf("mysqld restart count exceeds the limit: %d", mylet.RestartLimit)
			log.Error(err)
			return err
		}
		if mylet.restartCount > 0 {
			log.Infof("mysqld restart attempt %d...", mylet.restartCount)
			time.Sleep(5 * time.Second)
		}

		if err := mylet.cleanupStalePidAndSocket(); err != nil {
			log.Errorf("cleanup stale pid/socket files failed, aborting: %v", err)
			return err
		}

		cmd := mylet.Mysqld(context.Background())
		err := cmd.Start()
		if err != nil {
			log.Errorf("failed to start mysqld (retry %d/%d): %v", mylet.restartCount, mylet.RestartLimit, err)
			mylet.restartCount++
			continue
		}

		waitChan := make(chan error, 1)
		go func() {
			waitChan <- cmd.Wait()
		}()

		err = mylet.postStartChecks(cmd)
		if err != nil {
			log.Errorf("post-start checks failed (retry %d/%d): %v", mylet.restartCount, mylet.RestartLimit, err)
			// a graceful shutdown
			if cmd.Process != nil {
				if errSignal := cmd.Process.Signal(syscall.SIGTERM); errSignal != nil {
					log.Errorf("failed to send SIGTERM to mysqld after post-start failure (original error: %v): %v", err, errSignal)
				}
			}
			<-waitChan // wait for it to exit
			mylet.restartCount++
			continue
		}

		log.Info("mysqld is running and passed initial checks, entering monitor loop")
		mylet.restartCount = 0 // Reset restart count after a successful start

		select {
		case <-mylet.ExitChan:
			log.Info("mylet exiting, stopping mysqld...")
			if cmd.Process != nil {
				if errSignal := cmd.Process.Signal(syscall.SIGTERM); errSignal != nil {
					log.Errorf("failed to send SIGTERM to mysqld on exit: %v", errSignal)
				}
				<-waitChan
			}
			return nil
		case err = <-waitChan:
			log.Errorf("mysqld exited unexpectedly with error: %v. (retry %d/%d)", err, mylet.restartCount, mylet.RestartLimit)
			mylet.restartCount++
			mylet.LivenessProbe = false
			mylet.ReadinessProbe = false
			mylet.StartupProbe = false
		}
	}
}

func (mylet *Mylet) postStartChecks(cmd *exec.Cmd) error {
	var version string
	var err error

	for i := 1; i <= MaxStartup; i++ {
		// Check if the process exited prematurely
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return fmt.Errorf("mysqld exited during startup with code %d", cmd.ProcessState.ExitCode())
		}

		log.Infof("get mysqld version sleep 5 seconds %d", i)
		time.Sleep(Timeout5s)

		version, err = mylet.GetMysqldVersion(mylet.Spec.Id)
		if err != nil {
			log.Errorf("get mysqld version: %v", err)
		}
		if err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("failed to get mysqld version after max startups: %w", err)
	}

	mylet.StartupProbe = true

	err = mylet.CheckVersion(version)
	if err != nil {
		log.Errorf("check version: %v", err)
		return err
	}

	if mylet.IsPrimary() {
		err = mylet.SetupPrimary()
		if err != nil {
			log.Errorf("setup primary: %v", err)
			return err
		}
		log.Infof("start primary mysqld %s", version)

		if mylet.Mysql.Spec.EnableExporter {
			err = mylet.ExporterUser()
			if err != nil {
				log.Errorf("export user: %v", err)
			}
		}
	} else {
		err = mylet.SetupReplica()
		if err != nil {
			log.Errorf("setup replica: %v", err)
			return err
		}
		log.Infof("start replica mysqld %s", version)
	}

	mylet.LivenessProbe = true
	mylet.ReadinessProbe = true

	return nil
}

// TODO
func (mylet *Mylet) WaitRelay() error {
	return nil
}

// TODO
func (mylet *Mylet) CheckPosition() error {
	return nil
}

func AccessDenied(err error) bool {
	s := ""
	if err != nil {
		s = strings.ToLower(err.Error())
	}
	return strings.Contains(s, "access") && strings.Contains(s, "denied")
}

func (mylet *Mylet) FixPasswordId(id int) error {
	dsn := fmt.Sprintf("%s:%s%d@tcp(localhost:%d)/mysql",
		mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, id, mylet.Spec.Port)
	db, err := Open(dsn)
	if err != nil {
		return err
	}

	query := []string{
		"SET SESSION sql_log_bin = OFF;",
		"SET GLOBAL read_only = OFF;",
		"SET GLOBAL super_read_only = OFF;",
	}

	// q := fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY '%s%d';", mylet.Mysql.Spec.ReplicaUsername, mc.MyHosts(), mylet.Mysql.Spec.ReplicaPassword, mylet.Spec.Id)
	q := fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s%d';", mylet.Mysql.Spec.ReplicaUsername, mylet.Mysql.Spec.ReplicaPassword, mylet.Spec.Id)
	query = append(query, q)

	q = fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s%d';", mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, mylet.Spec.Id)
	query = append(query, q)

	query = append(query,
		"FLUSH PRIVILEGES;",

		"SET GLOBAL super_read_only = ON;",
		"SET GLOBAL read_only = ON;",
		"SET SESSION sql_log_bin = ON;",
	)

	ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
	defer cancel()

	_, err = db.ExecContext(ctx, strings.Join(query, "\n"))
	return err
}

func (mylet *Mylet) SetupPrimary() error {
	if !mylet.IsPrimary() {
		return fmt.Errorf("%s is not a primary", mylet.Spec.Name)
	}

	err := mylet.WaitRelay()
	if err != nil {
		return err
	}

	return mylet.SwitchPrimary(RW)
}

const (
	RW = true
	RO = false
)

func (mylet *Mylet) SwitchPrimary(rw bool) error {
	dsn := fmt.Sprintf("%s:%s%d@tcp(localhost:%d)/mysql",
		mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, mylet.Spec.Id, mylet.Spec.Port)
	db, err := Open(dsn)
	if err != nil {
		return err
	}

	ro := "ON"
	if rw {
		ro = "OFF"
	}

	query := []string{
		"SET SESSION sql_log_bin = OFF;",
		"SET GLOBAL super_read_only = " + ro + ";",
		"SET GLOBAL read_only = " + ro + ";",
		"SET SESSION sql_log_bin = ON;",
	}

	ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
	defer cancel()

	_, err = db.ExecContext(ctx, strings.Join(query, "\n"))
	return err
}

func (mylet *Mylet) SetupReplica() error {
	sourceId := *mylet.Spec.SourceId
	if sourceId == -1 {
		return fmt.Errorf("%s no source id", mylet.Spec.Name)
	}

	err := mylet.WaitRelay()
	if err != nil {
		return err
	}

	err = mylet.CheckPosition()
	if err != nil {
		return err
	}

	dsn := fmt.Sprintf("%s:%s%d@tcp(localhost:%d)/mysql",
		mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, mylet.Spec.Id, mylet.Spec.Port)
	db, err := Open(dsn)
	if err != nil {
		return err
	}

	query := []string{
		"SET SESSION sql_log_bin = OFF;",
		"SET GLOBAL read_only = OFF;",
		"SET GLOBAL super_read_only = OFF;",
	}

	// TODO FOR CHANNEL

	q := "CHANGE REPLICATION SOURCE TO SOURCE_HOST = '%s', SOURCE_PORT = %d, SOURCE_USER = '%s', SOURCE_PASSWORD = '%s%d', SOURCE_AUTO_POSITION = 1;"
	if mylet.Mysql.Status.Version.Major == 5 {
		q = "CHANGE MASTER TO MASTER_HOST = '%s', MASTER_PORT = %d, MASTER_USER = '%s', MASTER_PASSWORD = '%s%d', MASTER_AUTO_POSITION = 1;"
		query = append(query, "RESET SLAVE;")
	} else {
		query = append(query, "RESET REPLICA;")
	}
	q = fmt.Sprintf(q, mylet.Mysql.SoloShortHost(sourceId), mylet.Mysql.Spec.Port, mylet.Mysql.Spec.ReplicaUsername, mylet.Mysql.Spec.ReplicaPassword, sourceId)
	query = append(query, q)

	if mylet.Mysql.Status.Version.Major == 5 {
		q = "START SLAVE;"
	} else {
		q = "START REPLICA;"
	}
	query = append(query, q)

	query = append(query,
		"SET GLOBAL super_read_only = ON;",
		"SET GLOBAL read_only = ON;",
		"SET SESSION sql_log_bin = ON;",
	)

	ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
	defer cancel()

	_, err = db.ExecContext(ctx, strings.Join(query, "\n"))
	return err
}

func (mylet *Mylet) StopReplica() error {
	err := mylet.WaitRelay()
	if err != nil {
		return err
	}

	dsn := fmt.Sprintf("%s:%s%d@tcp(localhost:%d)/mysql",
		mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, mylet.Spec.Id, mylet.Spec.Port)
	db, err := Open(dsn)
	if err != nil {
		return err
	}

	query := []string{
		"SET SESSION sql_log_bin = OFF;",
		"SET GLOBAL read_only = OFF;",
		"SET GLOBAL super_read_only = OFF;",
	}

	if mylet.Mysql.Status.Version.Major == 5 {
		query = append(query, "STOP SLAVE;")
		query = append(query, "RESET SLAVE;")
	} else {
		query = append(query, "STOP REPLICA;")
		query = append(query, "RESET REPLICA;")
	}

	query = append(query,
		"SET GLOBAL super_read_only = ON;",
		"SET GLOBAL read_only = ON;",
		"SET SESSION sql_log_bin = ON;",
	)

	ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
	defer cancel()

	_, err = db.ExecContext(ctx, strings.Join(query, "\n"))
	return err
}

func (mylet *Mylet) StopPrimary() error {
	return mylet.SwitchPrimary(RO)
}

func (mylet *Mylet) ChangePrimary(newId int) (err error) {
	oldId := *mylet.Mysql.Spec.PrimaryId

	if mylet.Mysql.Spec.PrimaryMode != v1.ModeClassic || newId < 0 || newId >= mylet.Mysql.Spec.Size() || newId == oldId {
		return nil
	}

	log.Infoln("change primary", oldId, "to", newId)

	if mylet.Spec.Id == oldId {
		err = mylet.StopPrimary()
		if err != nil {
			return err
		}
	}

	sourceId := *mylet.Spec.SourceId
	if sourceId != -1 {
		err = mylet.StopReplica()
		if err != nil {
			return err
		}
	}

	mylet.Mysql.Spec.PrimaryId = pointer.IntPtr(newId)

	sourceId = -1
	if mylet.Spec.Id > newId {
		sourceId = mylet.Spec.Id - 1
	} else if mylet.Spec.Id < newId {
		sourceId = mylet.Spec.Id + 1
	}
	mylet.Spec.SourceId = pointer.IntPtr(sourceId)

	if mylet.IsPrimary() {
		err = mylet.SetupPrimary()
		if err != nil {
			return err
		}
	} else {
		err = mylet.SetupReplica()
		if err != nil {
			return err
		}
	}
	return err
}

func (mylet *Mylet) ExporterUser() error {
	if mylet.Mysql.Spec.ExporterUsername == "" || mylet.Mysql.Spec.ExporterPassword == "" {
		return fmt.Errorf("exporter username and password required")
	}
	if v1.HasQuote(mylet.Mysql.Spec.ExporterUsername, mylet.Mysql.Spec.ExporterPassword) {
		return fmt.Errorf("exporter username and password must not contains any quotation marks")
	}

	dsn := fmt.Sprintf("%s:%s%d@tcp(localhost:%d)/mysql",
		mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, mylet.Spec.Id, mylet.Spec.Port)
	db, err := Open(dsn)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
	defer cancel()

	var query []string

	n := 0
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM mysql.user WHERE user = '%s' AND host = 'localhost';", mylet.Mysql.Spec.ExporterUsername)).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		query = append(query, fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';", mylet.Mysql.Spec.ExporterUsername, mylet.Mysql.Spec.ExporterPassword))
	} else {
		query = append(query, fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED WITH mysql_native_password BY '%s' WITH MAX_USER_CONNECTIONS 3;", mylet.Mysql.Spec.ExporterUsername, mylet.Mysql.Spec.ExporterPassword))
	}

	query = append(query, fmt.Sprintf("GRANT PROCESS, REPLICATION CLIENT, REPLICATION SLAVE, SELECT ON *.* TO '%s'@'localhost';", mylet.Mysql.Spec.ExporterUsername))
	query = append(query, "FLUSH PRIVILEGES;")

	_, err = db.ExecContext(ctx, strings.Join(query, "\n"))
	return err
}
