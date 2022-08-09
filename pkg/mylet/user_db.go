package mylet

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cxr29/log"
	"github.com/cxr29/tiny"
	v1 "github.com/erda-project/mysql-operator/api/v1"
)

func (mylet *Mylet) _Run_SQL(ctx *tiny.Context) {
	const maxSize = 32 << 20 // 32 MB
	err := ctx.Request.ParseMultipartForm(maxSize)
	if err != nil {
		ctx.WriteError(err.Error())
		return
	}

	username, n := ctx.First("username")
	if n != 1 {
		ctx.WriteError("invalid username")
		return
	}

	password, n := ctx.First("password")
	if n != 1 {
		ctx.WriteError("invalid password")
		return
	}

	if username == "" || password == "" {
		ctx.WriteError("username and password required")
		return
	}

	dbname, n := ctx.First("dbname")
	if n != 0 && n != 1 {
		ctx.WriteError("invalid dbname")
		return
	}

	//TODO validate
	if v1.HasQuote(username, password, dbname) {
		ctx.WriteError("username, password and dbname must not contains any quotation marks")
		return
	}

	query := ctx.Request.Form["query"]
	if len(query) > 0 {
		dsn := fmt.Sprintf("%s:%s@tcp(localhost:%d)/%s",
			username, password, mylet.Spec.Port, dbname)
		db, err := Open(dsn)
		if err != nil {
			ctx.WriteError(err.Error())
			return
		}

		cxt, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()

		_, err = db.ExecContext(cxt, strings.Join(query, "\n"))
		if err != nil {
			ctx.WriteError(err.Error())
			return
		}
	}

	file := ctx.Request.MultipartForm.File["file"]
	if len(file) > 0 {
		d, err := os.MkdirTemp("", "run-sql-*")
		if err != nil {
			ctx.WriteError(err.Error())
			return
		}
		defer os.RemoveAll(d)

		var sqls []string

		copyFile := func(ext string, f *multipart.FileHeader) error {
			if ext == "" {
				return nil
			}

			w, err := os.CreateTemp(d, "*"+ext)
			if err != nil {
				return err
			}
			defer w.Close()

			r, err := f.Open()
			if err != nil {
				return err
			}

			_, err = io.Copy(w, r)
			if err != nil {
				return err
			}

			sqls = append(sqls, w.Name())
			return nil
		}

		for _, f := range file {
			if f.Size > maxSize {
				ctx.WriteError("file too large")
				return
			}

			var ext string
			for _, t := range []string{
				".sql",
				".sql.gz",
				".sql.bz2",
				".sql.xz",
				".sql.zst",
			} {
				if strings.HasSuffix(f.Filename, t) {
					ext = t
					break
				}
			}

			err = copyFile(ext, f)
			if err != nil {
				ctx.WriteError(err.Error())
				return
			}
		}

		for _, f := range sqls {
			err = mylet.ExecFile(username, password, dbname, f)
			if err != nil {
				ctx.WriteError(err.Error())
				return
			}
		}
	}

	ctx.WriteData(tiny.M{
		"query": len(query),
		"file":  len(file),
	})
}

const RunSQL = `
run_sql() {
	if [[ -n "$MYSQL_DATABASE" ]]; then
		set -- --database="$MYSQL_DATABASE" "$@"
	fi

	mysql --defaults-file="$MY_CNF" -h"$MYSQL_HOST" -P"$MYSQL_PORT" -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$@"
}

case "$MYSQL_FILE" in
	*.sql)     cat        "$MYSQL_FILE" | run_sql ;;
	*.sql.bz2) bunzip2 -c "$MYSQL_FILE" | run_sql ;;
	*.sql.gz)  gunzip -c  "$MYSQL_FILE" | run_sql ;;
	*.sql.xz)  xzcat      "$MYSQL_FILE" | run_sql ;;
	*.sql.zst) zstd -dc   "$MYSQL_FILE" | run_sql ;;
esac
`

func (mylet *Mylet) ExecFile(username, password, dbname, f string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash")
	cmd.Stdin = strings.NewReader(RunSQL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{
		"MY_CNF=" + mylet.MyCnf(),
		"MYSQL_HOST=127.0.0.1",
		"MYSQL_PORT=" + strconv.Itoa(mylet.Spec.Port),
		"MYSQL_USER=" + username,
		"MYSQL_PASSWORD=" + password,
		"MYSQL_DATABASE=" + dbname,
		"MYSQL_FILE=" + f,
	}

	log.Infoln(username, "run", dbname, "sql", f)
	return cmd.Run()
}

func (mylet *Mylet) _User_DB(ctx *tiny.Context) {
	username, n := ctx.First("username")
	if n != 0 && n != 1 {
		ctx.WriteError("invalid username")
		return
	}

	password, n := ctx.First("password")
	if n != 0 && n != 1 {
		ctx.WriteError("invalid password")
		return
	}

	if username == "" && password != "" {
		ctx.WriteError("username required")
		return
	}

	dbname, n := ctx.First("dbname")
	if n != 0 && n != 1 {
		ctx.WriteError("invalid dbname")
		return
	}

	if password == "" && dbname == "" {
		ctx.WriteError("password or/and dbname required")
		return
	}

	collation, n := ctx.First("collation")
	if n != 0 && n != 1 {
		ctx.WriteError("invalid collation")
		return
	}

	charset, n := ctx.First("charset")
	if n != 0 && n != 1 {
		ctx.WriteError("invalid charset")
		return
	}

	// TODO validate
	if v1.HasQuote(username, password, dbname, collation, charset) {
		ctx.WriteError("username, password and dbname must not contains any quotation marks")
		return
	}

	dsn := fmt.Sprintf("%s:%s%d@tcp(localhost:%d)/mysql",
		mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, mylet.Spec.Id, mylet.Spec.Port)
	db, err := Open(dsn)
	if err != nil {
		ctx.WriteError(err.Error())
		return
	}

	cxt, cancel := context.WithTimeout(context.Background(), Timeout5s)
	defer cancel()

	var query []string

	if username != "" && password != "" {
		n := 0
		err := db.QueryRowContext(cxt, fmt.Sprintf("SELECT COUNT(*) FROM mysql.user WHERE user = '%s' AND host = '%%';", username)).Scan(&n)
		if err != nil {
			ctx.WriteError(err.Error())
			return
		}
		if n > 0 {
			query = append(query, fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s';", username, password))
		} else {
			query = append(query, fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED WITH mysql_native_password BY '%s';", username, password))
		}
	}

	if dbname != "" {
		//TODO SHOW COLLATION
		//TODO ALTER
		if collation == "" {
			collation = "gb18030_chinese_ci"
			collation = "utf8mb4_general_ci"
		}
		if charset == "" {
			charset = "gb18030"
			charset = "utf8mb4"
		}

		q := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET '%s' COLLATE '%s';", dbname, charset, collation)
		query = append(query, q)

		if username != "" {
			q = fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%' WITH GRANT OPTION;", dbname, username)
			query = append(query, q)
		}
	}

	//TODO
	if username == "mysql" {
		q := "GRANT ALL PRIVILEGES ON *.* TO 'mysql'@'%' WITH GRANT OPTION;"
		query = append(query, q)
	}

	query = append(query, "FLUSH PRIVILEGES;")

	_, err = db.ExecContext(cxt, strings.Join(query, "\n"))
	if err != nil {
		ctx.WriteError(err.Error())
		return
	}

	ctx.WriteData(tiny.M{
		"username":  username,
		"password":  password, //TODO auto generate
		"dbname":    dbname,
		"collation": collation,
		"charset":   charset,
	})
}
