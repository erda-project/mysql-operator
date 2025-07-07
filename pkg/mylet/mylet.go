package mylet

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	v1 "github.com/erda-project/mysql-operator/api/v1"
)

// Mylet is a sidecar for mysql container
type Mylet struct {
	sync.Mutex
	sync.Once

	Mysql *v1.Mysql
	Spec  *v1.MysqlSoloSpec

	Backing   string
	MysqlSolo v1.MysqlSolo

	Running        bool
	LivenessProbe  bool
	ReadinessProbe bool
	StartupProbe   bool

	RestartLimit int
	restartCount int

	SwitchChan chan int
	ExitChan   chan struct{}

	hangCount int // 连续探测失败次数
}

// New creates a new Mylet
func New(mysql *v1.Mysql, id int) (*Mylet, error) {
	m := &Mylet{
		Mysql:      mysql,
		Spec:       &mysql.Spec.Solos[id],
		SwitchChan: make(chan int, 1),
		ExitChan:   make(chan struct{}),
	}

	restartLimitStr := os.Getenv("MYSQL_RESTART_LIMIT")
	if restartLimit, err := strconv.Atoi(restartLimitStr); err == nil && restartLimit > 0 {
		m.RestartLimit = restartLimit
	} else {
		// default restart limit
		m.RestartLimit = 5
	}

	return m, nil
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
