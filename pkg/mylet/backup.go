package mylet

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cxr29/log"
)

const (
	Hour8 = 8 * time.Hour
	Day   = 24 * time.Hour

	BackupFilename = "mylet_backup"
	DatetimeLayout = "20060102.150405"
	CompressExt    = ".tar.gz"
)

/*
name.date.time/
							base
							inc1
							inc...
*/
func (mylet *Mylet) GetBackupDir(t time.Time) string {
	dir := mylet.Spec.Name + "." + t.Format(DatetimeLayout)
	return filepath.Join(mylet.BackupDir(), dir)
}

// TODO
func (mylet *Mylet) CreateBackupUser() error {
	panic(`
CREATE USER '%s'@'localhost' IDENTIFIED BY '%s%d';
GRANT RELOAD, LOCK TABLES, PROCESS, REPLICATION CLIENT ON *.* TO '%s'@'localhost';
FLUSH PRIVILEGES;
`)
}

func WriteBackupInfo(dir string, t time.Time) error {
	// TODO username/password
	s := t.Format(DatetimeLayout) + " " + time.Since(t).String() + "\n"
	return os.WriteFile(filepath.Join(dir, BackupFilename), []byte(s), 0644)
}

func ReadBackupInfo(dir string) (t time.Time, d time.Duration, err error) {
	b, err := os.ReadFile(filepath.Join(dir, BackupFilename))
	if err != nil {
		return
	}

	s := strings.TrimSpace(string(b))
	i := strings.IndexByte(s, ' ')
	if i == -1 {
		err = fmt.Errorf("no space")
		return
	}

	t, err = time.ParseInLocation(DatetimeLayout, s[:i], time.Local)
	if err == nil {
		d, err = time.ParseDuration(s[i+1:])
	}

	return
}

// TODO time corrupt
func (mylet *Mylet) GetBackups() (a []time.Time, err error) {
	prefix := mylet.Spec.Name + "."

	dir := mylet.BackupDir()
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if path == dir {
			return nil
		}

		if err != nil {
			return err
		}

		if !d.IsDir() {
			return nil
		}

		s := filepath.Base(path)
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			t, err := time.ParseInLocation(DatetimeLayout, s, time.Local)
			if err == nil {
				r, _, err := ReadBackupInfo(filepath.Join(path, "base"))
				if err == nil && t.Equal(r) {
					a = append(a, t)
				}
			}
		}

		return filepath.SkipDir
	})

	sort.Slice(a, func(i, j int) bool {
		return a[i].Before(a[j])
	})

	return
}

func GetIncrementals(dir string) (a []int, err error) {
	p := "inc"

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if path == dir {
			return nil
		}

		if err != nil {
			return err
		}

		if !d.IsDir() {
			return nil
		}

		s := filepath.Base(path)
		if strings.HasPrefix(s, p) {
			s = strings.TrimPrefix(s, p)
			i, err := strconv.Atoi(s)
			if err == nil {
				_, _, err := ReadBackupInfo(path)
				if err == nil {
					a = append(a, i)
				}
			}
		}

		return filepath.SkipDir
	})

	sort.Ints(a)

	return
}

func (mylet *Mylet) GetCompresses() (a []time.Time, err error) {
	prefix := mylet.Spec.Name + "."

	dir := mylet.BackupDir()
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if path == dir {
			return nil
		}

		if err != nil {
			return err
		}

		if d.IsDir() {
			return filepath.SkipDir
		}

		s := filepath.Base(path)
		if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, CompressExt) {
			s = strings.TrimPrefix(s, prefix)
			s = strings.TrimSuffix(s, CompressExt)
			t, err := time.ParseInLocation(DatetimeLayout, s, time.Local)
			if err == nil {
				a = append(a, t)
			}
		}

		return nil
	})

	sort.Slice(a, func(i, j int) bool {
		return a[i].Before(a[j])
	})

	return
}

func (mylet *Mylet) LockBackup(s string) string {
	mylet.Lock()
	defer mylet.Unlock()
	o := mylet.Backing
	if o == "" {
		mylet.Backing = s
	}
	return o
}
func (mylet *Mylet) UnlockBackup() {
	mylet.Lock()
	mylet.Backing = ""
	mylet.Unlock()
}

func (mylet *Mylet) FullBackup() (time.Time, error) {
	o := mylet.LockBackup("full backup")
	if o != "" {
		return time.Time{}, fmt.Errorf("backing: %s", o)
	}
	defer mylet.UnlockBackup()

	ctx, cancel := context.WithTimeout(context.Background(), Hour8)
	defer cancel()

	now := time.Now()
	dt := now.Format(DatetimeLayout)
	dir := mylet.GetBackupDir(now)
	err := os.MkdirAll(dir, 0700)
	if err != nil {
		return time.Time{}, err
	}
	dir = filepath.Join(dir, "base")

	log.Infoln("start full backup", mylet.Spec.Name, dt)

	cmd := exec.CommandContext(ctx, "xtrabackup",
		"--defaults-file="+mylet.MyCnf(),

		"--host=127.0.0.1",
		"--port="+strconv.Itoa(mylet.Spec.Port),
		"--user="+mylet.Mysql.Spec.LocalUsername,
		"--password="+mylet.Mysql.Spec.LocalPassword+strconv.Itoa(mylet.Spec.Id),

		"--backup",
		"--target-dir="+dir,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err == nil {
		err = WriteBackupInfo(dir, now)
	}
	if err == nil {
		log.Infoln("end full backup", mylet.Spec.Name, dt)
	} else {
		log.Errorln("end full backup", mylet.Spec.Name, dt, err)
	}

	return now, err
}

func (mylet *Mylet) IncrementalBackup(t time.Time) (int, error) {
	o := mylet.LockBackup("incremental backup")
	if o != "" {
		return 0, fmt.Errorf("backing: %s", o)
	}
	defer mylet.UnlockBackup()

	ctx, cancel := context.WithTimeout(context.Background(), Hour8)
	defer cancel()

	dt := t.Format(DatetimeLayout)
	dir := mylet.GetBackupDir(t)

	a, err := GetIncrementals(dir)
	if err != nil {
		return 0, err
	}

	i := len(a) - 1
	if i == -1 {
		i = 0
	} else {
		i = a[i]
	}

	baseDir := filepath.Join(dir, "base")
	if i > 0 {
		baseDir = filepath.Join(dir, "inc"+strconv.Itoa(i))
	}

	i++
	dir = filepath.Join(dir, "inc"+strconv.Itoa(i))

	now := time.Now()
	log.Infoln("start incremental backup", mylet.Spec.Name, dt, i)

	cmd := exec.CommandContext(ctx, "xtrabackup",
		"--defaults-file="+mylet.MyCnf(),

		"--host=127.0.0.1",
		"--port="+strconv.Itoa(mylet.Spec.Port),
		"--user="+mylet.Mysql.Spec.LocalUsername,
		"--password="+mylet.Mysql.Spec.LocalPassword+strconv.Itoa(mylet.Spec.Id),

		"--backup",
		"--target-dir="+dir,
		"--incremental-basedir="+baseDir,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err == nil {
		err = WriteBackupInfo(dir, now)
	}
	if err == nil {
		log.Infoln("end incremental backup", mylet.Spec.Name, dt, i)
	} else {
		log.Errorln("end incremental backup", mylet.Spec.Name, dt, i, err)
	}

	return i, err
}

func (mylet *Mylet) CompressLastBackup() (string, error) {
	a, err := mylet.GetBackups()
	if err != nil {
		return "", err
	}
	i := len(a) - 1
	if i == -1 {
		return "", fmt.Errorf("no backups")
	}
	dir := mylet.GetBackupDir(a[i])
	return mylet.CompressBackup(dir)
}

func (mylet *Mylet) CompressBackup(dir string) (string, error) {
	o := mylet.LockBackup("compress backup")
	if o != "" {
		return "", fmt.Errorf("backing: %s", o)
	}
	defer mylet.UnlockBackup()

	ctx, cancel := context.WithTimeout(context.Background(), Hour8)
	defer cancel()

	p := filepath.Dir(dir)
	d := filepath.Base(dir)
	f := d + CompressExt
	t := "cxrtmp." + f

	cmd := exec.CommandContext(ctx, "tar", "-czf", t, d)
	cmd.Dir = p
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	f = filepath.Join(p, f)
	t = filepath.Join(p, t)

	err := cmd.Run()
	if err == nil {
		err = os.Rename(t, f)
	}

	return f, err
}

func (mylet *Mylet) PrepareLastBackup() error {
	a, err := mylet.GetBackups()
	if err != nil {
		return err
	}
	i := len(a) - 1
	if i == -1 {
		return fmt.Errorf("no backups")
	}
	dir := mylet.GetBackupDir(a[i])
	return mylet.PrepareBackup(mylet.Spec.Id, dir)
}

func (mylet *Mylet) PrepareBackup(id int, dir string) error {
	o := mylet.LockBackup("prepare backup")
	if o != "" {
		return fmt.Errorf("backing: %s", o)
	}
	defer mylet.UnlockBackup()

	a, err := GetIncrementals(dir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	args := []string{
		"--defaults-file=" + mylet.MyCnf(),

		"--host=127.0.0.1",
		"--port=" + strconv.Itoa(mylet.Spec.Port), //
		"--user=" + mylet.Mysql.Spec.LocalUsername,
		"--password=" + mylet.Mysql.Spec.LocalPassword + strconv.Itoa(id),

		"--prepare",
		"--target-dir=" + filepath.Join(dir, "base"),
	}
	if len(a) > 0 {
		args = append(args, "-apply-log-only")
	}

	cmd := exec.CommandContext(ctx, "xtrabackup", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return err
	}

	for i, inc := range a {
		args := []string{
			"--defaults-file=" + mylet.MyCnf(),

			"--host=127.0.0.1",
			"--port=" + strconv.Itoa(mylet.Spec.Port), //
			"--user=" + mylet.Mysql.Spec.LocalUsername,
			"--password=" + mylet.Mysql.Spec.LocalPassword + strconv.Itoa(id),

			"--prepare",
			"--target-dir=" + filepath.Join(dir, "base"),
			"--incremental-dir=" + filepath.Join(dir, "inc"+strconv.Itoa(inc)),
		}
		if i+1 < len(a) {
			args = append(args, "-apply-log-only")
		}

		cmd := exec.CommandContext(ctx, "xtrabackup", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			break
		}
	}

	return err
}

func (mylet *Mylet) RestoreBackup(id int, dir string, move bool) error {
	o := mylet.LockBackup("restore backup")
	if o != "" {
		return fmt.Errorf("backing: %s", o)
	}
	defer mylet.UnlockBackup()

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	args := []string{
		"--defaults-file=" + mylet.MyCnf(),

		"--host=127.0.0.1",
		"--port=" + strconv.Itoa(mylet.Spec.Port), //
		"--user=" + mylet.Mysql.Spec.LocalUsername,
		"--password=" + mylet.Mysql.Spec.LocalPassword + strconv.Itoa(id),
	}
	if move {
		args = append(args, "--move-back")
	} else {
		args = append(args, "--copy-back")
	}
	args = append(args, "--target-dir="+dir) // base

	cmd := exec.CommandContext(ctx, "xtrabackup", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (mylet *Mylet) AdjustBackup(id int) error {
	o := mylet.LockBackup("adjust backup")
	if o != "" {
		return fmt.Errorf("backing: %s", o)
	}
	defer mylet.UnlockBackup()

	gtid, err := mylet.ReadGtid()
	if err != nil {
		return err
	}

	return mylet.adjustBackup(id, gtid)
}

func (mylet *Mylet) adjustBackup(id int, gtid string) error {
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

	dsn := fmt.Sprintf("%s:%s%d@unix(%s)/mysql", mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, id, socket)
	db, err := Open(dsn)
	if err != nil {
		return err
	}

	query := []string{
		"SET SESSION sql_log_bin = OFF;",
		"SET GLOBAL read_only = OFF;",
		"SET GLOBAL super_read_only = OFF;",
	}

	query = append(query, "RESET MASTER;")
	q := fmt.Sprintf("SET GLOBAL gtid_purged = '%s';", gtid)
	log.Infoln(q)
	query = append(query, q)

	// q = fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY '%s%d';", mylet.Mysql.Spec.ReplicaUsername, mylet.Mysql.Spec.MyHosts(), mylet.Mysql.Spec.ReplicaPassword, mylet.Spec.Id)
	q = fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s%d';", mylet.Mysql.Spec.ReplicaUsername, mylet.Mysql.Spec.ReplicaPassword, mylet.Spec.Id)
	query = append(query, q)

	q = fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s%d';", mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, mylet.Spec.Id)
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
			log.ErrFatal(err, "adjust backup")
		}()

		if err == nil {
			break
		}
	}

	return err
}

func (mylet *Mylet) ReadGtid() (gtid string, err error) {
	b, err := ioutil.ReadFile(filepath.Join(mylet.DataDir(), "xtrabackup_info"))
	if err != nil {
		return
	}
	s := string(b)

	p := "GTID of the last change"
	i := strings.Index(s, p)
	if i == -1 {
		// return "", fmt.Errorf("no gtid prefix")
		return mylet.Read_binlog_pos()
	}
	s = s[i+len(p):]
	i = strings.IndexByte(s, '\'')
	if i == -1 || strings.TrimSpace(s[:i]) != "" {
		return "", fmt.Errorf("no gtid left single quote")
	}
	s = s[i+1:]
	i = strings.IndexByte(s, '\'')
	if i == -1 {
		return "", fmt.Errorf("no gtid right single quote")
	}
	s = s[:i]

	var a []string
	for _, s := range strings.Split(s, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			a = append(a, s)
		}
	}
	return strings.Join(a, ","), nil
}

// TODO: multiline
func (mylet *Mylet) Read_binlog_pos() (gtid string, err error) {
	f, err := os.Open(filepath.Join(mylet.DataDir(), "xtrabackup_info"))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		s := scanner.Text()
		i := strings.IndexByte(s, '=')
		if i == -1 {
			continue
		}
		if strings.TrimSpace(s[:i]) == "binlog_pos" {
			a := strings.Split(s[i+1:], ",")
			if len(a) != 2 && //no gtid
				len(a) != 3 {
				err = fmt.Errorf("binlog_pos length %d", len(a))
				return
			}
			keys := [...]string{
				"filename",
				"position",
				"GTID of the last change",
			}
			values := [3]string{}
			for i, s := range a {
				s = strings.TrimSpace(s)
				j := strings.IndexByte(s, '\'')
				l := len(s) - 1
				if j == -1 || j == l || s[l] != '\'' {
					err = fmt.Errorf("binlog_pos single quote: %s", s)
					return
				}
				k := strings.TrimSpace(s[:j])
				v := s[j+1 : l]
				if k != keys[i] {
					err = fmt.Errorf("binlog_pos expect %s but %s", keys[i], k)
					return
				}
				values[i] = v
			}
			gtid = values[2]
			break
		}
	}

	err = scanner.Err()
	return
}
