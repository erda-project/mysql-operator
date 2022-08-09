package mylet

import (
	"os"
	"path/filepath"
	"text/template"
	"time"
)

func (mylet *Mylet) BackupFile(src string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	dir := mylet.BackupDir()
	err := os.MkdirAll(dir, 0700)
	if err != nil {
		return err
	}

	dst := filepath.Base(src) + time.Now().Format(DatetimeLayout)
	return os.Rename(src, filepath.Join(dir, dst))
}

func (mylet *Mylet) Configure() error {
	myCnf := mylet.MyCnf()

	/*TODO diff
	err := mylet.BackupFile(myCnf)
	if err != nil {
		return err
	}
	*/

	f, err := os.OpenFile(myCnf, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	t := template.New(filepath.Base(f.Name()))
	t, err = t.Parse(MyCnfTmpl)
	if err == nil {
		err = t.Execute(f, mylet)
	}

	return err
}

// TODO
func CheckMyCnf() {
}
func CheckMysqldCmdVersion() {
}
