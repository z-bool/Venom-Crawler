package main

import (
	"Venom-Crawler/internal/runner"
	"Venom-Crawler/pkg/katana/types"
	"github.com/ttacon/chalk"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"
)

func katanaRun(options *types.Options) {
	katanaRunner, err := runner.New(options)
	if err != nil || katanaRunner == nil {
		log.Println(chalk.Green.Color("error: katana不能创建执行器, " + err.Error()))
	}
	defer katanaRunner.Close()

	// close handler
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		for range c {
			log.Println(chalk.Yellow.Color("- Ctrl + C 在终端被按下"))
			katanaRunner.Close()
			os.Exit(0)
		}
	}()

	if err := katanaRunner.ExecuteCrawling(); err != nil {
		log.Println(chalk.Red.Color("error: katana爬行器不能被执行, " + err.Error()))
	}
}

func parseUrl(_url string) string {
	u, err := url.Parse(_url)
	if err != nil {
		log.Println(chalk.Red.Color("error: " + _url + "不能被正常解析"))
	}
	baseURL := u.Scheme + "://" + u.Host
	return baseURL
}

func dealUrlScope(urls []string) []string {
	var newUrls []string
	for _, _url := range urls {
		newUrl := parseUrl(_url)
		if newUrl != "" {
			newUrls = append(newUrls, newUrl)
		}
	}
	return newUrls
}
