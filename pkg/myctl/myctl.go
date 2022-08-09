package myctl

import (
	"sync"

	v1 "github.com/erda-project/mysql-operator/api/v1"
	"github.com/erda-project/mysql-operator/pkg/mylet"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type Myctl struct {
	sync.Mutex

	C chan event.GenericEvent
	M map[types.NamespacedName]*MysqlGroup

	ReadinessProbe bool
	LivenessProbe  bool
	StartupProbe   bool
}

func NewMyctl() *Myctl {
	ctl := &Myctl{
		C: make(chan event.GenericEvent),
		M: make(map[types.NamespacedName]*MysqlGroup, 10),
	}
	go ctl.Run()
	return ctl
}

func (ctl *Myctl) SyncStatus(mysql *v1.Mysql) error {
	g, err := ctl.GetOrNewGroup(mysql)
	if err != nil {
		return err
	}

	mysql.Spec = *g.Spec.DeepCopy()
	mysql.Status = *g.Status.DeepCopy()

	return nil
}

func (ctl *Myctl) GetOrNewGroup(mysql *v1.Mysql) (*MysqlGroup, error) {
	k := mysql.NamespacedName()

	ctl.Lock()
	defer ctl.Unlock()

	g, ok := ctl.M[k]
	if ok {
		return g, nil
	}

	g = &MysqlGroup{
		Myctl:    ctl,
		Mysql:    mysql.DeepCopy(),
		ExitChan: make(chan struct{}, 1),
	}

	if err := g.Validate(); err != nil {
		return nil, err
	}

	g.Status.Color = v1.Yellow
	for i := range g.Status.Solos {
		g.Status.Solos[i].Status.Color = v1.Yellow
	}

	writeId := *g.Spec.PrimaryId
	if writeId == -1 {
		writeId = 0
	}
	g.Status.WriteId = pointer.IntPtr(writeId)

	readId := g.Spec.Primaries
	if *g.Spec.Replicas == 0 {
		readId--
	}
	g.Status.ReadId = pointer.IntPtr(readId)

	n := g.Spec.Size() + 1
	g.States = make(map[mylet.StateKey]*mylet.MysqlState, n*n)

	ctl.M[k] = g

	go g.Start()

	mysql.Spec = *g.Spec.DeepCopy()
	mysql.Status = *g.Status.DeepCopy()

	return g, nil
}

func (ctl *Myctl) SyncSpec(mysql *v1.Mysql) error {
	g, err := ctl.GetOrNewGroup(mysql)
	if err == nil {
		err = g.Diff(mysql)
	}
	if err != nil {
		return err
	}

	mysql.Spec = *g.Spec.DeepCopy()
	mysql.Status = *g.Status.DeepCopy()

	return nil
}

func (ctl *Myctl) Purge(k types.NamespacedName) error {
	ctl.Lock()
	defer ctl.Unlock()

	g, ok := ctl.M[k]
	if ok {
		g.ExitChan <- struct{}{}
		delete(ctl.M, k)
	}

	return nil
}
