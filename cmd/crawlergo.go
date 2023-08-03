package main

import (
	"Venom-Crawler/internal/utils"
	"Venom-Crawler/pkg/crawlergo"
	"Venom-Crawler/pkg/crawlergo/config"
	"Venom-Crawler/pkg/crawlergo/model"
	"Venom-Crawler/pkg/crawlergo/tools"
	"Venom-Crawler/pkg/crawlergo/tools/requests"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/panjf2000/ants/v2"
	"github.com/ttacon/chalk"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func dealUrl(_url string) model.Request {
	var req model.Request
	url, err := model.GetUrl(_url)
	if err != nil {
		log.Println(chalk.Red.Color("error: 请求" + "url" + "失败, " + err.Error()))
	}
	if postData != "" {
		req = model.GetRequest(config.POST, url, getOption())
	} else {
		req = model.GetRequest(config.GET, url, getOption())
	}
	req.Proxy = taskConfig.Proxy
	return req
}

func crawlergoRun() {
	signalChan = make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
	if taskConfig.URL == "" && len(taskConfig.URLList) == 0 {
		fmt.Println("error: URL和URL集合文件必须得有一个")
		return
	}
	var targets []*model.Request
	if taskConfig.URL != "" {
		req := dealUrl(taskConfig.URL)
		targets = append(targets, &req)
	}
	if len(taskConfig.URLList) > 0 {
		for _, v := range taskConfig.URLList {
			req := dealUrl(v)
			targets = append(targets, &req)
		}
	}

	taskConfig.IgnoreKeywords = ignoreKeywords.Value()
	if taskConfig.Proxy != "" {
		log.Println(chalk.Green.Color("爬虫请求代理为: " + taskConfig.Proxy))
	}

	if len(targets) == 0 {
		return
	}

	var err error

	// 检查自定义的表单参数配置
	taskConfig.CustomFormValues, err = parseCustomFormValues(customFormTypeValues.Value())
	if err != nil {
		log.Println(chalk.Red.Color("error: 自定义键值数据解析出错1"))
	}
	taskConfig.CustomFormKeywordValues, err = keywordStringToMap(customFormKeywordValues.Value())
	if err != nil {
		log.Println(chalk.Red.Color("error: 自定义键值数据解析出错2"))
	}

	// 开始爬虫任务
	task, err := crawlergo.NewCrawlerTask(targets, taskConfig)
	if err != nil {
		log.Println(chalk.Red.Color("error: 创建爬行任务失败"))
		os.Exit(-1)
	}

	// 提示自定义表单填充参数
	if len(taskConfig.CustomFormValues) > 0 {
		log.Println(chalk.Green.Color("自定义参数1: " + tools.MapStringFormat(taskConfig.CustomFormValues)))
	}
	// 提示自定义表单填充参数
	if len(taskConfig.CustomFormKeywordValues) > 0 {
		log.Println(chalk.Green.Color("自定义参数2: " + tools.MapStringFormat(taskConfig.CustomFormKeywordValues)))
	}

	if _, ok := taskConfig.CustomFormValues["default"]; !ok {
		taskConfig.CustomFormValues["default"] = config.DefaultInputText
	}
	go handleExit(task)
	task.Run()
	result := task.Result

	// 内置请求代理
	if pushAddress != "" {
		Push2Proxy(result.ReqList)
	}

	// 输出结果
	outputResult(result)

}

func getOption() model.Options {
	var option model.Options
	if postData != "" {
		option.PostData = postData
	}
	if taskConfig.ExtraHeadersString != "" {
		err := json.Unmarshal([]byte(taskConfig.ExtraHeadersString), &taskConfig.ExtraHeaders)
		if err != nil {
			log.Println(chalk.Red.Color("error: 自定义参数头不能被序列化"))
		}
		option.Headers = taskConfig.ExtraHeaders
	}
	return option
}

func parseCustomFormValues(customData []string) (map[string]string, error) {
	parsedData := map[string]string{}
	for _, item := range customData {
		keyValue := strings.Split(item, "=")
		if len(keyValue) < 2 {
			return nil, errors.New("error: invalid form item: " + item)
		}
		key := keyValue[0]
		if !tools.StringSliceContain(config.AllowedFormName, key) {
			return nil, errors.New("error: not allowed form key: " + key)
		}
		value := keyValue[1]
		parsedData[key] = value
	}
	return parsedData, nil
}

func keywordStringToMap(data []string) (map[string]string, error) {
	parsedData := map[string]string{}
	for _, item := range data {
		keyValue := strings.Split(item, "=")
		if len(keyValue) < 2 {
			return nil, errors.New("error: invalid keyword format: " + item)
		}
		key := keyValue[0]
		value := keyValue[1]
		parsedData[key] = value
	}
	return parsedData, nil
}

func outputResult(result *crawlergo.Result) {
	for _, req := range result.ReqList {
		//req.FormatPrint()
		utils.AppendToFile("crawlergo-result.txt", req.URL.String())
	}
}

/*
*
原生被动代理推送支持
*/
func Push2Proxy(reqList []*model.Request) {
	pool, _ := ants.NewPool(pushProxyPoolMax)
	defer pool.Release()
	for _, req := range reqList {
		task := ProxyTask{
			req:       req,
			pushProxy: pushAddress,
		}
		pushProxyWG.Add(1)
		go func() {
			err := pool.Submit(task.doRequest)
			if err != nil {
				log.Println(chalk.Red.Color("error: 加入流量转发任务失败: " + err.Error()))
				pushProxyWG.Done()
			}
		}()
	}
	pushProxyWG.Wait()
}

/*
*
协程池请求的任务
*/
func (p *ProxyTask) doRequest() {
	defer pushProxyWG.Done()
	_, _ = requests.Request(p.req.Method, p.req.URL.String(), tools.ConvertHeaders(p.req.Headers), []byte(p.req.PostData),
		&requests.ReqOptions{Timeout: 1, AllowRedirect: false, Proxy: p.pushProxy})
}

func handleExit(t *crawlergo.CrawlerTask) {
	<-signalChan
	t.Pool.Tune(1)
	t.Pool.Release()
	t.Browser.Close()
	os.Exit(-1)
}
