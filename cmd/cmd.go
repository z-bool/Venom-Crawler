package main

import (
	"Venom-Crawler/internal/utils"
	"Venom-Crawler/pkg/crawlergo"
	"Venom-Crawler/pkg/crawlergo/config"
	"Venom-Crawler/pkg/crawlergo/model"
	"Venom-Crawler/pkg/katana/types"
	"flag"
	"fmt"
	"github.com/ttacon/chalk"
	"github.com/urfave/cli/v2"
	"log"
	"math"
	"os"
	"strings"
	"sync"
)

type Result struct {
	ReqList       []Request `json:"req_list"`
	AllReqList    []Request `json:"all_req_list"`
	AllDomainList []string  `json:"all_domain_list"`
	SubDomainList []string  `json:"sub_domain_list"`
}

type Request struct {
	Url     string                 `json:"url"`
	Method  string                 `json:"method"`
	Headers map[string]interface{} `json:"headers"`
	Data    string                 `json:"data"`
	Source  string                 `json:"source"`
}

type ProxyTask struct {
	req       *model.Request
	pushProxy string
}

func existCheck(filename string) {
	if _, err := os.Stat(filename); err == nil {
		err = os.Remove(filename)
		if err != nil {
			log.Fatal(chalk.Red.Color("error: " + err.Error()))
		}
	}
}
func startCheck() {
	arr := []string{"katana-result.txt", "result-all.txt", "crawlergo-result.txt"}
	for _, s := range arr {
		existCheck(s)
	}
}

var (
	taskConfig              crawlergo.TaskConfig
	outputMode              string
	postData                string
	signalChan              chan os.Signal
	ignoreKeywords          = cli.NewStringSlice(config.DefaultIgnoreKeywords...)
	customFormTypeValues    = cli.NewStringSlice()
	customFormKeywordValues = cli.NewStringSlice()
	pushAddress             string
	pushProxyPoolMax        int
	pushProxyWG             sync.WaitGroup
	outputJsonPath          string
	urlScope                []string
)

func cmd() {
	fmt.Print(chalk.Magenta.NewStyle().Style(`
VVVVVVVV           VVVVVVVV                                                                           
V::::::V           V::::::V                                                                           
V::::::V           V::::::V                                                                           
V::::::V           V::::::V                                                                           
 V:::::V           V:::::V eeeeeeeeeeee    nnnn  nnnnnnnn       ooooooooooo      mmmmmmm    mmmmmmm   
  V:::::V         V:::::Vee::::::::::::ee  n:::nn::::::::nn   oo:::::::::::oo  mm:::::::m  m:::::::mm 
   V:::::V       V:::::Ve::::::eeeee:::::een::::::::::::::nn o:::::::::::::::om::::::::::mm::::::::::m
    V:::::V     V:::::Ve::::::e     e:::::enn:::::::::::::::no:::::ooooo:::::om::::::::::::::::::::::m
     V:::::V   V:::::V e:::::::eeeee::::::e  n:::::nnnn:::::no::::o     o::::om:::::mmm::::::mmm:::::m
      V:::::V V:::::V  e:::::::::::::::::e   n::::n    n::::no::::o     o::::om::::m   m::::m   m::::m
       V:::::V:::::V   e::::::eeeeeeeeeee    n::::n    n::::no::::o     o::::om::::m   m::::m   m::::m
        V:::::::::V    e:::::::e             n::::n    n::::no::::o     o::::om::::m   m::::m   m::::m
         V:::::::V     e::::::::e            n::::n    n::::no:::::ooooo:::::om::::m   m::::m   m::::m
          V:::::V       e::::::::eeeeeeee    n::::n    n::::no:::::::::::::::om::::m   m::::m   m::::m
           V:::V         ee:::::::::::::e    n::::n    n::::n oo:::::::::::oo m::::m   m::::m   m::::m
            VVV            eeeeeeeeeeeeee    nnnnnn    nnnnnn   ooooooooooo   mmmmmm   mmmmmm   mmmmmm
                                                                                                      
                                      		毒液系列——为Venom-Transponder而生的缝合怪                                                                
                                            							by 阿呆安全团队
                                            							公众号：阿呆攻防
`))
	isHeadless := flag.Bool("headless", false, chalk.Green.Color("浏览器是否可见"))
	chromium := flag.String("chromium", "", chalk.Green.Color("无头浏览器chromium路径配置"))
	customHeaders := flag.String("headers", "{\"User-Agent\": \""+config.DefaultUA+"\"}", chalk.Green.Color("自定义请求头参数，要以json格式被序列化"))
	maxCrawler := flag.Int("maxCrawler", config.MaxCrawlCount, chalk.Green.Color("URL启动的任务最大的爬行个数"))
	mode := flag.String("mode", "smart", chalk.Green.Color("爬行模式，simple/smart/strict,默认smart"))
	proxy := flag.String("proxy", "", chalk.Green.Color("请求的代理，针对访问URL在墙外的情况，默认直连为空"))
	blackKey := flag.String("blackKey", "", chalk.Green.Color("黑名单关键词，用于避免被爬虫执行危险操作，用,分割，如：logout,delete,update"))
	url := flag.String("url", "", chalk.Green.Color("执行爬行的单个URL"))
	urlTxt := flag.String("urlTxtPath", "", chalk.Green.Color("如果需求是批量爬行URL，那需要将URL写入txt，然后将路径放入"))
	encode := flag.Bool("encodeUrlWithCharset", false, chalk.Green.Color("是否对URL进行编码"))
	depth := flag.Int("depth", 3, chalk.Green.Color("最大爬行深度，默认是3"))
	flag.Parse()
	startCheck()
	options := &types.Options{}
	if *urlTxt == "" && *url == "" {
		log.Println(chalk.Red.Color("URL文件和URL必须有一个！！！"))
		os.Exit(0)
	}
	var urls []string
	if *urlTxt != "" {
		urlList := utils.GetUrlListFromTxt(*urlTxt)
		if len(urlList) > 0 {
			urlList = utils.UniqueUrls(urlList)
			urls = urlList
			options.URLs = urlList
			urlScope = dealUrlScope(urlList)
		}
	}
	options.MaxDepth = *depth
	options.Headless = true

	if *mode == "simple" {
		options.ScrapeJSResponses = false
		options.AutomaticFormFill = false
	} else {
		options.ScrapeJSResponses = true
		options.AutomaticFormFill = true
	}
	options.KnownFiles = ""
	options.BodyReadSize = math.MaxInt
	options.Timeout = 10
	options.Retries = 1
	options.Proxy = *proxy

	// 请求头要单独将json处理为键值对,目前不设置
	options.Strategy = "depth-first"
	options.ShowBrowser = *isHeadless
	if *chromium != "" {
		options.SystemChromePath = *chromium
	}
	options.FieldScope = "rdn"
	options.OutputFile = "katana-result.txt"
	if *url != "" {
		newUrl := parseUrl(*url)
		if newUrl != "" {
			urlScope = append(urlScope, newUrl)
		}
		options.URLs = append(urls, *url)
	}
	options.Scope = utils.UniqueUrls(urlScope)
	options.Concurrency = 10
	options.Parallelism = 10
	options.RateLimit = 150
	options.ExtensionFilter = []string{"css", "jpg", "jpeg", "png", "ico", "gif", "webp", "mp3", "mp4", "ttf", "tif", "tiff", "woff", "woff2"}
	katanaRun(options)

	// 执行crawlergo之前将结果文件读取

	urls = utils.GetUrlListFromTxt("katana-result.txt")
	if len(urls) > 0 {
		taskConfig.URLList = dealUrlScope(urls)
	}
	// Crawlergo配置
	ignoreList := make([]string, 0)
	taskConfig.NoHeadless = *isHeadless
	taskConfig.ChromiumPath = *chromium
	taskConfig.Proxy = *proxy
	taskConfig.EncodeURLWithCharset = *encode
	taskConfig.FilterMode = *mode
	taskConfig.MaxCrawlCount = *maxCrawler
	taskConfig.ExtraHeadersString = *customHeaders
	taskConfig.MaxTabsCount = config.MaxTabsCount
	taskConfig.PathFromRobots = true
	taskConfig.TabRunTimeout = config.TabRunTimeout
	taskConfig.DomContentLoadedTimeout = config.DomContentLoadedTimeout
	taskConfig.EventTriggerMode = config.EventTriggerAsync
	taskConfig.EventTriggerInterval = config.EventTriggerInterval
	taskConfig.BeforeExitDelay = config.BeforeExitDelay
	taskConfig.MaxRunTime = config.MaxRunTime
	taskConfig.CustomFormValues = map[string]string{}
	if *blackKey != "" {
		ignoreList = strings.Split(*blackKey, ",")
	}
	taskConfig.IgnoreKeywords = ignoreList
	crawlergoRun()

	//全部程序执行完之后将三个文件进行合并，这里暂时只有两个
	finalResult := make([]string, 0)
	arr := []string{"katana-result.txt", "crawlergo-result.txt"}
	for _, filename := range arr {
		fromTxt := utils.GetUrlListFromTxt(filename)
		for _, _url := range fromTxt {
			finalResult = append(finalResult, _url)
		}
	}
	for _, _url := range utils.UniqueUrls(finalResult) {
		utils.AppendToFile("result-all.txt", _url)
	}
}

func main() {
	cmd()
}
