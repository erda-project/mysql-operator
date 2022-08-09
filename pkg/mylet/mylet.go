package mylet

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	v1 "github.com/erda-project/mysql-operator/api/v1"
)

type Mylet struct {
	sync.Mutex

	Mysql *v1.Mysql
	v1.MysqlSolo

	ReadinessProbe bool
	LivenessProbe  bool
	StartupProbe   bool

	Backing    string
	Running    bool
	ExitChan   chan struct{}
	SwitchChan chan int
}

func (mylet *Mylet) BackupDir() string {
	return filepath.Join(mylet.Spec.Mydir, "backup")
}
func (mylet *Mylet) MyCnf() string {
	return filepath.Join(mylet.Spec.Mydir, "my.cnf")
}
func (mylet *Mylet) DataDir() string {
	return filepath.Join(mylet.Spec.Mydir, "mysql")
}
func (mylet *Mylet) Socket() string {
	return filepath.Join(mylet.DataDir(), mylet.Spec.Name+".sock")
}
func (mylet *Mylet) Mysqld(ctx context.Context, args ...string) *exec.Cmd {
	args = append([]string{"--defaults-file=" + mylet.MyCnf()}, args...)

	cmd := exec.CommandContext(ctx, "mysqld", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd
}

func (mylet *Mylet) IsPrimary() bool {
	if mylet.Mysql.Spec.PrimaryMode == v1.ModeClassic {
		return mylet.Spec.Id == *mylet.Mysql.Spec.PrimaryId
	}
	return mylet.Spec.Id < mylet.Mysql.Spec.Primaries
}
func (mylet *Mylet) IsReplica() bool {
	return !mylet.IsPrimary()
}
