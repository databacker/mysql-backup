package database

import (
	"fmt"

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
	config.Net = "tcp"
	config.ParseTime = true
	config.Addr = fmt.Sprintf("%s:%d", c.Host, c.Port)
	return config.FormatDSN()
}
