package dev_server

import (
	"log"
)

func HelloWorld(accessTokenStr string) {
	log.Printf("Hello world! We'd be using %s as your access token.", accessTokenStr)
}
