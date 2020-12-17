package main

import (
	"fmt"
	"log"
	"os"
	"resume/util"
)

func main() {
	//URL := "https://github.com/*****/download/releases/download/v2.1.5/*******-2.1.5.dmg"

	//URL := "https://www.python.org/ftp/python/3.9.0/python-3.9.0-macosx10.9.pkg"
	URL := "https://golang.org/dl/go1.15.5.linux-amd64.tar.gz"

	wd, err := os.Getwd() //获取当前工作目录
	if err != nil {
		log.Println(err)
	}
	fmt.Println(util.DownloadFile(URL, wd+"/file"))

}
