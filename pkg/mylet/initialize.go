package mylet

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cxr29/log"
)

func IsEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}

	return false, err
}

func (mylet *Mylet) FetchAndPrepare() error {
	dir := mylet.DataDir()
	empty, err := IsEmpty(dir)
	if err != nil {
		return err
	}
	if !empty {
		return fmt.Errorf("mysql datadir not empty: %s", dir)
	}

	id := *mylet.Spec.SourceId
	if id == -1 {
		id = 0
	}
	if mylet.Spec.Id == id {
		return fmt.Errorf("self fetch: %d", id)
	}
	s := mylet.Mysql.Status.Solos[id]

	t := dir + ".cxrtmp"
	err = os.RemoveAll(t)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), Hour8)
	defer cancel()

	u := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(s.Spec.Host, strconv.Itoa(s.Spec.MyletPort)),
		Path:   "/api/addons/mylet/download/backup",
	}
	q := make(url.Values, 1)
	q.Set("datetime", "replication")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Token", SoloToken(mylet.Mysql, mylet.Spec.Name))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status code %d", res.StatusCode)
	}

	// TODO
	filename := res.Header.Get("Content-Disposition")
	log.Infoln("download backup", filename)

	err = os.MkdirAll(t, 0755)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "tar", "-xzf", "-", "-C", t, "--strip-components=1")
	cmd.Dir = t
	cmd.Stdin = res.Body
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return err
	}

	err = mylet.PrepareBackup(id, t)
	if err == nil {
		err = mylet.RestoreBackup(id, filepath.Join(t, "base"), true)
		if err == nil {
			err = mylet.AdjustBackup(id)
		}
	}
	if err != nil {
		return err
	}

	return os.RemoveAll(t)
}

func (mylet *Mylet) Initialize() error {
	dir := mylet.DataDir()
	empty, err := IsEmpty(dir)
	if err != nil {
		return err
	}
	if !empty {
		return fmt.Errorf("mysql datadir not empty: %s", dir)
	}
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), Timeout1m)
	defer cancel()

	cmd := mylet.Mysqld(ctx, "--initialize-insecure")
	err = cmd.Run()
	if err != nil {
		return err
	}

	err = mylet.RenameRoot()
	if err == nil {
		err = mylet.ChangeLocalPassword()
		if err == nil {
			err = mylet.InitDB()
		}
	}

	return err
}

func (mylet *Mylet) RenameRoot() error {
	if mylet.Mysql.Spec.LocalUsername == "root" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), Timeout1m)
	defer cancel()

	socket := mylet.Socket()
	cmd := mylet.Mysqld(ctx,
		"--skip-networking",
		// "--socket="+socket,
	)

	err := cmd.Start()
	if err != nil {
		return err
	}

	defer func() {
		err := cmd.Process.Signal(syscall.SIGTERM)
		log.ErrError(err, "stop local mysqld")
		err = cmd.Wait()
		log.ErrError(err, "wait local mysqld")
	}()

	dsn := fmt.Sprintf("root:@unix(%s)/mysql", socket)
	db, err := Open(dsn)
	if err != nil {
		return err
	}

	query := []string{
		"SET SESSION sql_log_bin = OFF;",
		"SET GLOBAL read_only = OFF;",
		"SET GLOBAL super_read_only = OFF;",
	}

	// q := fmt.Sprintf("RENAME USER 'root'@'localhost' TO '%s'@'localhost';", mylet.LocalUsername)
	q := fmt.Sprintf("UPDATE mysql.user SET user = '%s' where user = 'root' AND host = 'localhost';", mylet.Mysql.Spec.LocalUsername)
	query = append(query, q)

	query = append(query,
		"FLUSH PRIVILEGES;",

		"SET GLOBAL super_read_only = ON;",
		"SET GLOBAL read_only = ON;",
		"SET SESSION sql_log_bin = ON;",
	)

	for i := 1; i <= 10; i++ {
		log.Infoln("ping local mysqld sleep 5 seconds", i)
		time.Sleep(Timeout5s)

		func() {
			ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
			defer cancel()

			err = db.PingContext(ctx)
			if err != nil {
				return
			}

			_, err = db.ExecContext(ctx, strings.Join(query, "\n"))
			log.ErrFatal(err, "rename root")
		}()

		if err == nil {
			break
		}
	}

	return err
}

func (mylet *Mylet) ChangeLocalPassword() error {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout1m)
	defer cancel()

	socket := mylet.Socket()
	cmd := mylet.Mysqld(ctx,
		"--skip-networking",
		// "--socket="+socket,
	)

	err := cmd.Start()
	if err != nil {
		return err
	}

	defer func() {
		err := cmd.Process.Signal(syscall.SIGTERM)
		log.ErrError(err, "stop local mysqld")
		err = cmd.Wait()
		log.ErrError(err, "wait local mysqld")
	}()

	dsn := fmt.Sprintf("%s:@unix(%s)/mysql", mylet.Mysql.Spec.LocalUsername, socket)
	db, err := Open(dsn)
	if err != nil {
		return err
	}

	query := []string{
		"SET SESSION sql_log_bin = OFF;",
		"SET GLOBAL read_only = OFF;",
		"SET GLOBAL super_read_only = OFF;",
	}

	q := fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED WITH mysql_native_password BY '%s%d';", mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, mylet.Spec.Id)
	query = append(query, q)

	query = append(query,
		"FLUSH PRIVILEGES;",

		"SET GLOBAL super_read_only = ON;",
		"SET GLOBAL read_only = ON;",
		"SET SESSION sql_log_bin = ON;",
	)

	for i := 1; i <= 10; i++ {
		log.Infoln("ping local mysqld sleep 5 seconds", i)
		time.Sleep(Timeout5s)

		func() {
			ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
			defer cancel()

			err = db.PingContext(ctx)
			if err != nil {
				return
			}

			_, err = db.ExecContext(ctx, strings.Join(query, "\n"))
			log.ErrFatal(err, "change local password")
		}()

		if err == nil {
			break
		}
	}

	return err
}

func (mylet *Mylet) InitDB() error {
	q, err := LoadTimeZone()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), Timeout1m)
	defer cancel()

	socket := mylet.Socket()
	cmd := mylet.Mysqld(ctx,
		"--skip-networking",
		// "--socket="+socket,
	)

	err = cmd.Start()
	if err != nil {
		return err
	}

	defer func() {
		err := cmd.Process.Signal(syscall.SIGTERM)
		log.ErrError(err, "stop local mysqld")
		err = cmd.Wait()
		log.ErrError(err, "wait local mysqld")
	}()

	dsn := fmt.Sprintf("%s:%s%d@unix(%s)/mysql", mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, mylet.Spec.Id, socket)
	db, err := Open(dsn)
	if err != nil {
		return err
	}

	query := []string{
		"SET SESSION sql_log_bin = OFF;",
		"SET GLOBAL read_only = OFF;",
		"SET GLOBAL super_read_only = OFF;",

		"DROP USER IF EXISTS 'root'@'%';",
		"DROP DATABASE IF EXISTS test;",
	}

	query = append(query, q)

	q = fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED WITH mysql_native_password BY '%s%d';", mylet.Mysql.Spec.ReplicaUsername, mylet.Mysql.Spec.ReplicaPassword, mylet.Spec.Id)
	query = append(query, q)

	q = fmt.Sprintf("GRANT REPLICATION CLIENT, REPLICATION SLAVE ON *.* TO '%s'@'%%';", mylet.Mysql.Spec.ReplicaUsername)
	query = append(query, q)

	query = append(query,
		"FLUSH PRIVILEGES;",

		"SET GLOBAL super_read_only = ON;",
		"SET GLOBAL read_only = ON;",
		"SET SESSION sql_log_bin = ON;",
	)

	for i := 1; i <= 10; i++ {
		log.Infoln("ping local mysqld sleep 5 seconds", i)
		time.Sleep(Timeout5s)

		func() {
			ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
			defer cancel()

			err = db.PingContext(ctx)
			if err != nil {
				return
			}

			_, err = db.ExecContext(ctx, strings.Join(query, "\n"))
			log.ErrFatal(err, "init db")
		}()

		if err == nil {
			break
		}
	}

	return err
}

func LoadTimeZone() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
	defer cancel()

	var buf bytes.Buffer

	cmd := exec.CommandContext(ctx, "mysql_tzinfo_to_sql", "/usr/share/zoneinfo")
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return buf.String(), nil
}
