package mylet

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cxr29/log"
	v1 "github.com/erda-project/mysql-operator/api/v1"
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

func (mylet *Mylet) Start() error {
	mylet.Lock()
	if mylet.Running {
		mylet.Unlock()
		return fmt.Errorf("running")
	} else {
		mylet.Running = true
	}
	mylet.Unlock()

	cmd := mylet.Mysqld(context.Background())
	err := cmd.Start()
	defer func() {
		if cmd.Process != nil {
			if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
				err := cmd.Process.Signal(syscall.SIGTERM)
				log.ErrError(err, "stop mysqld")
			}
			err := cmd.Wait()
			log.ErrError(err, "wait mysqld")
		}

		mylet.Lock()
		mylet.Running = false
		mylet.Unlock()
	}()
	if err != nil {
		return err
	}

	var version string
	// var fixPasswordId bool

	for i := 1; i <= MaxStartup; i++ {
		log.Infoln("get mysqld version sleep 5 seconds", i)
		time.Sleep(Timeout5s)

		version, err = mylet.GetMysqldVersion(mylet.Spec.Id)
		// sourceId := *mylet.Spec.SourceId
		// if AccessDenied(err) && sourceId != -1 {
		// 	version, err = mylet.GetMysqldVersion(sourceId)
		// 	if AccessDenied(err) {
		// 		return fmt.Errorf("access denied for user '%s'@'localhost'", mylet.Mysql.Spec.LocalUsername)
		// 	}
		// 	fixPasswordId = err == nil
		// }

		log.ErrError(err, "get mysqld version")
		if err == nil {
			break
		}

		if cmd.Process != nil && cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			//TODO fix socket error, access denied...
			return fmt.Errorf("mysqld exited %d", cmd.ProcessState.ExitCode())
		}
	}

	mylet.StartupProbe = true

	err = mylet.CheckVersion(version)
	log.ErrError(err, "check version")
	if err != nil {
		return err
	}

	// if fixPasswordId {
	// 	err = mylet.FixPasswordId()
	// 	log.ErrError(err, "fix password id")
	// 	if err != nil {
	// 		return err
	// 	}
	// }

	if mylet.IsPrimary() {
		err = mylet.SetupPrimary()
		log.ErrError(err, "setup primary")
		if err != nil {
			return err
		}
		log.Infoln("start primary mysqld", version)

		if mylet.Mysql.Spec.EnableExporter {
			err = mylet.ExporterUser()
			log.ErrError(err, "export user")
		}
	} else {
		err = mylet.SetupReplica()
		log.ErrError(err, "setup replica")
		if err != nil {
			return err
		}
		log.Infoln("start replica mysqld", version)
	}

	mylet.LivenessProbe = true

	hang := 0
	timer := time.NewTicker(Timeout5s)
	defer timer.Stop()
	states := make(map[int]*MysqlState, mylet.Mysql.Spec.Size())

	for {
		select {
		case <-mylet.ExitChan:
			log.Infoln("start exit")
			return nil
		case newId := <-mylet.SwitchChan:
			err = mylet.ChangePrimary(newId)
			if err != nil {
				return err
			}
		case <-timer.C:
			mylet.Lock()
			if hang > 0 {
				log.Errorln("hang", hang)
			}
			hang++
			mylet.Unlock()

			go func() {
				err = mylet.SelfCheck()
				log.ErrError(err, "self check")
				mylet.ReadinessProbe = err == nil

				r := &MysqlReport{
					Name:     mylet.Spec.Name,
					SizeSpec: NewSizeSpec(mylet.Mysql),
				}
				defer func() {
					ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
					defer cancel()

					err := mylet.SendReport(ctx, r)
					if err != nil {
						time.Sleep(100 * time.Millisecond)
						err = mylet.SendReport(ctx, r)
					}
					log.ErrError(err, "send report")
				}()

				ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
				defer cancel()

				now := time.Now()
				m := CrossCheck(ctx, mylet.Mysql)

				mylet.Lock()
				defer mylet.Unlock()

				for id, err := range m {
					v, ok := states[id]
					if !ok {
						v = &MysqlState{
							StateKey: StateKey{
								FromId: mylet.Spec.Id,
								ToId:   id,
							},
						}
						states[id] = v
					}

					if err == nil {
						v.ErrorCount = 0
						v.GreenTime = now
					} else {
						if v.ErrorCount%10 == 1 {
							log.Errorln("from", mylet.Mysql.SoloName(v.FromId), "to", mylet.Mysql.SoloName(v.ToId), err)
						}

						v.ErrorCount++
						v.RedTime = now
						v.LastError = err.Error()
					}

					// Monotonic
					v.GreenDuration = now.Sub(v.GreenTime)
					v.RedDuration = now.Sub(v.RedTime)
				}

				for _, v := range states {
					b, err := json.Marshal(v)
					log.ErrError(err, "never")
					r.States = append(r.States, json.RawMessage(b))
				}

				hang--
				r.Hang = hang
			}()
		}
	}
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
		log.ErrError(err, "stop primary")
		if err != nil {
			return err
		}
	}

	sourceId := *mylet.Spec.SourceId
	if sourceId != -1 {
		err = mylet.StopReplica()
		log.ErrError(err, "stop replica")
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
		log.ErrError(err, "setup primary")
	} else {
		err = mylet.SetupReplica()
		log.ErrError(err, "setup replica")
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
