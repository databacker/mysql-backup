package database

import (
	"fmt"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
)

type Connection struct {
	User string
	Pass string
	Host string
	Port int
}

func (c Connection) MySQL() string {
	config := mysql.NewConfig()
	config.User = c.User
	config.Passwd = c.Pass
	if strings.HasPrefix(c.Host, "/") {
		config.Net = "unix"
		config.Addr = c.Host
	} else {
		config.Net = "tcp"
		config.Addr = fmt.Sprintf("%s:%d", c.Host, c.Port)
	}
	config.ParseTime = true
	return config.FormatDSN()
}
