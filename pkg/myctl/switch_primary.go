package myctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/cxr29/log"
	v1 "github.com/erda-project/mysql-operator/api/v1"
	"github.com/erda-project/mysql-operator/pkg/mylet"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const ( //TODO Spec
	Timeout5s     = 5 * time.Second
	Timeout15s    = 15 * time.Second
	SwitchCount   = 2
	MyctlReplicas = 1
)

func (g *MysqlGroup) Color(id int) (red, yellow, green int) {
	for _, s := range g.States {
		if s.ToId == id && s.YellowDuration < Timeout15s {
			if s.ErrorCount > 0 && s.RedDuration < Timeout15s {
				red++
			} else if s.ErrorCount == 0 && s.GreenDuration < Timeout15s {
				green++
			}
		}
	}
	yellow = (g.Spec.Size() + MyctlReplicas) - green - red
	return
}

func (g *MysqlGroup) SwitchPrimary(newId int) error {
	n := g.Spec.Size()
	if newId < 0 || newId >= n {
		return fmt.Errorf("primary id out of range")
	}

	now := time.Now()
	if now.Sub(g.SwitchTime) < Timeout15s {
		return fmt.Errorf("too frequently")
	}

	log.Infoln(g.Name, "switch primary", *g.Status.WriteId, "to", newId)

	g.Spec.PrimaryId = pointer.IntPtr(newId)
	g.Status.WriteId = pointer.IntPtr(newId)
	//TODO readId

	g.SwitchTime = now
	g.SwitchCount = 0

	g.C <- event.GenericEvent{Object: g.Mysql}

	f := func() bool {
		m := SwitchPrimaryAll(g.Mysql)
		y := 0
		for id, e := range m {
			if e == nil {
				y++
			} else {
				log.Errorln(g.Name, id, "switch primary", e)
			}
		}
		return 2*y >= n
	}

	//lint:ignore SA4000 retry
	if !f() && !f() {
		log.Noticeln("poor execution")
	}

	return nil
}

func SwitchPrimary(ctx context.Context, mysql *v1.Mysql, id int) error {
	s := mysql.Status.Solos[id]

	u := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(s.Spec.Host, strconv.Itoa(s.Spec.MyletPort)),
		Path:   "/api/addons/mylet/switch/primary/" + strconv.Itoa(*mysql.Spec.PrimaryId),
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Token", mylet.SoloToken(mysql, mysql.BuildName("myctl")))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status code %d, body: %s", res.StatusCode, string(b))
	}

	var v struct {
		Data  json.RawMessage
		Error interface{}
	}

	err = json.Unmarshal(b, &v)
	if err != nil {
		return err
	}

	if v.Error != nil {
		return fmt.Errorf("return error: %s", v.Error)
	}

	// v.Data

	return nil
}

func SwitchPrimaryAll(mysql *v1.Mysql) map[int]error {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
	defer cancel()

	n := mysql.Spec.Size()
	m := make(map[int]error, n)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			err := SwitchPrimary(ctx, mysql, id)
			mu.Lock()
			m[id] = err
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	return m
}
