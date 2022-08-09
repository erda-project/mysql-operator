package mylet

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	v1 "github.com/erda-project/mysql-operator/api/v1"
)

func (mylet *Mylet) SelfCheck() error {
	/*TODO
	if mylet.IsPrimary() {
		//show binary logs;
	} else {
		//show slave status;
	}
	*/
	return nil
}

// TODO more check
func DailCheck(ctx context.Context, addr string) error {
	var d net.Dialer

	n, err := d.DialContext(ctx, "tcp", addr)
	if err == nil {
		err = n.Close()
	}
	if err == nil {
		return nil
	}

	// retry
	time.Sleep(250 * time.Millisecond)

	n, err = d.DialContext(ctx, "tcp", addr)
	if err == nil {
		err = n.Close()
	}
	return err
}

func CrossCheck(ctx context.Context, mysql *v1.Mysql) map[int]error {
	n := mysql.Spec.Size()
	m := make(map[int]error, n)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			s := mysql.Status.Solos[id].Spec
			err := DailCheck(ctx, net.JoinHostPort(s.Host, strconv.Itoa(s.Port)))

			mu.Lock()
			m[id] = err
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	return m
}
