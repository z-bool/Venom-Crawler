package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/Qianlitp/crawlergo/pkg"
	"github.com/Qianlitp/crawlergo/pkg/config"
	"github.com/Qianlitp/crawlergo/pkg/logger"
	model2 "github.com/Qianlitp/crawlergo/pkg/model"
	"github.com/Qianlitp/crawlergo/pkg/tools"
	"github.com/Qianlitp/crawlergo/pkg/tools/requests"
	"github.com/panjf2000/ants/v2"
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/gologger/formatter"
	"github.com/projectdiscovery/gologger/levels"
	"github.com/projectdiscovery/katana/pkg/engine"
	"github.com/projectdiscovery/katana/pkg/engine/hybrid"
	"github.com/projectdiscovery/katana/pkg/engine/parser"
	"github.com/projectdiscovery/katana/pkg/output"
	"github.com/projectdiscovery/katana/pkg/types"
	"github.com/projectdiscovery/katana/pkg/utils"
	errorutil "github.com/projectdiscovery/utils/errors"
	fileutil "github.com/projectdiscovery/utils/file"
	urlutil "github.com/projectdiscovery/utils/url"
	"github.com/remeh/sizedwaitgroup"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"go.uber.org/multierr"
	"gopkg.in/yaml.v2"
)

const (
	DefaultMaxPushProxyPoolMax = 10
	DefaultLogLevel            = "Info"
)

var (
	taskConfig              pkg.TaskConfig
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
	logLevel                string
	domain                  string
	urls                    = make([]string, 0)
)

// Runner creates the required resources for crawling
// and executes the crawl process.
type Runner struct {
	crawlerOptions *types.CrawlerOptions
	stdin          bool
	crawler        engine.Engine
	options        *types.Options
}

func (r *Runner) ExecuteCrawling() error {
	inputs := r.parseInputs()
	if len(inputs) == 0 {
		return errorutil.New("no input provided for crawling")
	}

	defer r.crawler.Close()

	wg := sizedwaitgroup.New(r.options.Parallelism)
	for _, input := range inputs {
		wg.Add()
		input = addSchemeIfNotExists(input)
		go func(input string) {
			defer wg.Done()

			if err := r.crawler.Crawl(input); err != nil {
				gologger.Warning().Msgf("Could not crawl %s: %s", input, err)
			}
		}(input)
	}
	wg.Wait()
	return nil
}

// scheme less urls are skipped and are required for headless mode and other purposes
// this method adds scheme if given input does not have any
func addSchemeIfNotExists(inputURL string) string {
	if strings.HasPrefix(inputURL, urlutil.HTTP) || strings.HasPrefix(inputURL, urlutil.HTTPS) {
		return inputURL
	}
	parsed, err := urlutil.Parse(inputURL)
	if err != nil {
		gologger.Warning().Msgf("input %v is not a valid url got %v", inputURL, err)
		return inputURL
	}
	if parsed.Port() != "" && (parsed.Port() == "80" || parsed.Port() == "8080") {
		return urlutil.HTTP + urlutil.SchemeSeparator + inputURL
	} else {
		return urlutil.HTTPS + urlutil.SchemeSeparator + inputURL
	}
}

func validateOptions(options *types.Options) error {
	if options.MaxDepth <= 0 && options.CrawlDuration <= 0 {
		return errorutil.New("either max-depth or crawl-duration must be specified")
	}
	if len(options.URLs) == 0 && !fileutil.HasStdin() {
		return errorutil.New("no inputs specified for crawler")
	}
	if (options.HeadlessOptionalArguments != nil || options.HeadlessNoSandbox || options.SystemChromePath != "") && !options.Headless {
		return errorutil.New("headless mode (-hl) is required if -ho, -nos or -scp are set")
	}
	if options.SystemChromePath != "" {
		if !fileutil.FileExists(options.SystemChromePath) {
			return errorutil.New("specified system chrome binary does not exist")
		}
	}
	if options.StoreResponseDir != "" && !options.StoreResponse {
		gologger.Debug().Msgf("store response directory specified, enabling \"sr\" flag automatically\n")
		options.StoreResponse = true
	}
	for _, mr := range options.OutputMatchRegex {
		cr, err := regexp.Compile(mr)
		if err != nil {
			return errorutil.NewWithErr(err).Msgf("Invalid value for match regex option")
		}
		options.MatchRegex = append(options.MatchRegex, cr)
	}
	for _, fr := range options.OutputFilterRegex {
		cr, err := regexp.Compile(fr)
		if err != nil {
			return errorutil.NewWithErr(err).Msgf("Invalid value for filter regex option")
		}
		options.FilterRegex = append(options.FilterRegex, cr)
	}
	gologger.DefaultLogger.SetFormatter(formatter.NewCLI(options.NoColors))
	return nil
}

// readCustomFormConfig reads custom form fill config
func readCustomFormConfig(options *types.Options) error {
	file, err := os.Open(options.FormConfig)
	if err != nil {
		return errorutil.NewWithErr(err).Msgf("could not read form config")
	}
	defer file.Close()

	var data utils.FormFillData
	if err := yaml.NewDecoder(file).Decode(&data); err != nil {
		return errorutil.NewWithErr(err).Msgf("could not decode form config")
	}
	utils.FormData = data
	return nil
}

// parseInputs parses the inputs returning a slice of URLs
func (r *Runner) parseInputs() []string {
	values := make(map[string]struct{})
	for _, url := range r.options.URLs {
		value := normalizeInput(url)
		if _, ok := values[value]; !ok {
			values[value] = struct{}{}
		}
	}
	if r.stdin {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			value := normalizeInput(scanner.Text())
			if _, ok := values[value]; !ok {
				values[value] = struct{}{}
			}
		}
	}
	final := make([]string, 0, len(values))
	for k := range values {
		final = append(final, k)
	}
	return final
}

func normalizeInput(value string) string {
	return strings.TrimSpace(value)
}

// configureOutput configures the output logging levels to be displayed on the screen
func configureOutput(options *types.Options) {
	if options.Silent {
		gologger.DefaultLogger.SetMaxLevel(levels.LevelSilent)
	} else if options.Verbose {
		gologger.DefaultLogger.SetMaxLevel(levels.LevelWarning)
	} else if options.Debug {
		gologger.DefaultLogger.SetMaxLevel(levels.LevelDebug)
	} else {
		gologger.DefaultLogger.SetMaxLevel(levels.LevelInfo)
	}

	// logutil.DisableDefaultLogger()
}

func initExampleFormFillConfig() error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return errorutil.NewWithErr(err).Msgf("could not get home directory")
	}
	defaultConfig := filepath.Join(homedir, ".config", "katana", "form-config.yaml")

	if fileutil.FileExists(defaultConfig) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(defaultConfig), 0775); err != nil {
		return err
	}
	exampleConfig, err := os.Create(defaultConfig)
	if err != nil {
		return errorutil.NewWithErr(err).Msgf("could not get home directory")
	}
	defer exampleConfig.Close()

	err = yaml.NewEncoder(exampleConfig).Encode(utils.DefaultFormFillData)
	return err
}

func RemoveDuplicates(nums []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, num := range nums {
		if !seen[num] {
			seen[num] = true
			result = append(result, num)
		}
	}
	return result
}

// New returns a new crawl runner structure
func New(options *types.Options) (*Runner, error) {
	configureOutput(options)

	if err := initExampleFormFillConfig(); err != nil {
		return nil, errorutil.NewWithErr(err).Msgf("could not init default config")
	}
	if err := validateOptions(options); err != nil {
		return nil, errorutil.NewWithErr(err).Msgf("could not validate options")
	}
	if options.FormConfig != "" {
		if err := readCustomFormConfig(options); err != nil {
			return nil, err
		}
	}
	crawlerOptions, err := types.NewCrawlerOptions(options)
	if err != nil {
		return nil, errorutil.NewWithErr(err).Msgf("could not create crawler options")
	}

	parser.InitWithOptions(options)

	var crawler engine.Engine

	crawler, err = hybrid.New(crawlerOptions)

	if err != nil {
		return nil, errorutil.NewWithErr(err).Msgf("could not create standard crawler")
	}
	runner := &Runner{options: options, stdin: fileutil.HasStdin(), crawlerOptions: crawlerOptions, crawler: crawler}

	return runner, nil
}

// Close closes the runner releasing resources
func (r *Runner) Close() error {
	return multierr.Combine(
		r.crawler.Close(),
		r.crawlerOptions.Close(),
	)
}

func main() {
	app := &cli.App{
		Name:   "ADSEC-CRAWLER",
		Flags:  cliFlags,
		Action: run,
	}

	err := app.Run(os.Args)
	if err != nil {
		logger.Logger.Fatal(err)
	}
	domain = strings.TrimSuffix(domain, "/")
	options := &types.Options{}
	options.URLs = []string{domain}
	options.MaxDepth = 3
	options.FieldScope = "rdn"
	options.DisplayOutScope = false
	options.CrawlDuration = 0
	options.KnownFiles = ""
	options.BodyReadSize = 2 * 1024 * 1024
	options.Timeout = 10
	options.Retries = 1
	options.Proxy = ""
	options.CustomHeaders = []string{"User-Agent: \"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/114.0\""}
	options.RateLimit = 150
	options.FormConfig = ""
	options.FieldConfig = ""
	options.Strategy = "depth-first"
	options.AutomaticFormFill = true
	options.Headless = true
	options.IgnoreQueryParams = true
	options.UseInstalledChrome = false
	options.HeadlessNoSandbox = false
	options.ChromeDataDir = ""
	options.SystemChromePath = ""
	options.HeadlessNoIncognito = false
	options.NoScope = false
	options.Fields = ""
	options.StoreFields = ""
	options.Concurrency = 10
	options.Parallelism = 10
	options.RateLimitMinute = 0
	options.Delay = 0
	options.ScrapeJSResponses = false
	options.ExtensionFilter = []string{"png", "jpg", "gif", "ico", "jpeg", "mp3", "mp4", "webp", "ttf", "css", "remove", "delete"}
	options.OutputFilterRegex = []string{"js"}
	// options.SystemChromePath = "C:/Users/root/AppData/Roaming/rod/browser/chromium-1131003/chrome.exe"
	options.SystemChromePath = taskConfig.ChromiumPath
	options.ShowBrowser = false
	options.OnResult = func(result output.Result) {
		if result.Response != nil {
			if result.Response.StatusCode == 200 {
				logger.Logger.Infoln(result.Request.URL)
				urls = append(urls, result.Request.URL)
			}
		}
	}
	katanaRunner, err := New(options)
	if err != nil || katanaRunner == nil {
		gologger.Fatal().Msgf("不能创建runner: %s\n", err)
	}
	defer katanaRunner.Close()

	if err := katanaRunner.ExecuteCrawling(); err != nil {
		gologger.Fatal().Msgf("不能执行爬行: %s", err)
	}

	urlsResult := RemoveDuplicates(urls)
	gologger.Info().Msg("========接下来是去重后的最终结果======")
	for _, v := range urlsResult {
		fmt.Println(v)
	}
	WriteArrayToFile(urlsResult)
}

func run(c *cli.Context) error {
	signalChan = make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
	if domain == "" {
		return errors.New("必须要输入域名，如：-d baidu.com")
	}

	// 设置日志输出级别
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logger.Logger.Fatal(err)
	}
	logger.Logger.SetLevel(level)

	var targets []*model2.Request
	var req model2.Request
	url, err := model2.GetUrl(domain)
	if err != nil {
		logger.Logger.Error("转换URL出错", err)
	}
	if postData != "" {
		req = model2.GetRequest(config.POST, url, getOption())
	} else {
		req = model2.GetRequest(config.GET, url, getOption())
	}
	req.Proxy = taskConfig.Proxy
	targets = append(targets, &req)

	taskConfig.IgnoreKeywords = ignoreKeywords.Value()
	if taskConfig.Proxy != "" {
		logger.Logger.Info("请求通过代理: ", taskConfig.Proxy)
	}

	if len(targets) == 0 {
		logger.Logger.Fatal("该URL不能正常访问")
	}

	// 检查自定义的表单参数配置
	taskConfig.CustomFormValues, err = parseCustomFormValues(customFormTypeValues.Value())
	if err != nil {
		logger.Logger.Fatal(err)
	}
	taskConfig.CustomFormKeywordValues, err = keywordStringToMap(customFormKeywordValues.Value())
	if err != nil {
		logger.Logger.Fatal(err)
	}

	// 开始爬虫任务
	task, err := pkg.NewCrawlerTask(targets, taskConfig)
	if err != nil {
		logger.Logger.Error("创建爬行任务失败！！！")
		os.Exit(-1)
	}

	// 提示自定义表单填充参数
	if len(taskConfig.CustomFormValues) > 0 {
		logger.Logger.Info("自定义表单填充参数： " + tools.MapStringFormat(taskConfig.CustomFormValues))
	}
	// 提示自定义表单填充参数
	if len(taskConfig.CustomFormKeywordValues) > 0 {
		logger.Logger.Info("自定义表单填充参数的值： " + tools.MapStringFormat(taskConfig.CustomFormKeywordValues))
	}
	if _, ok := taskConfig.CustomFormValues["default"]; !ok {
		logger.Logger.Info("如果没有设置自定义填充表单，所有值会设置成默认: admin")
		taskConfig.CustomFormValues["default"] = "admin"
	}

	go handleExit(task)
	task.Run()
	result := task.Result

	// 内置请求代理
	if pushAddress != "" {
		logger.Logger.Info("推送结果到", pushAddress, ",最大线程:", pushProxyPoolMax)
		Push2Proxy(result.ReqList)
	}

	// 输出结果
	outputResult(result)

	return nil
}

func getOption() model2.Options {
	var option model2.Options
	if postData != "" {
		option.PostData = postData
	}
	if taskConfig.ExtraHeadersString != "" {
		err := json.Unmarshal([]byte(taskConfig.ExtraHeadersString), &taskConfig.ExtraHeaders)
		if err != nil {
			logger.Logger.Fatal("自定义请求头不能被JSON序列化")
			panic(err)
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
			return nil, errors.New("item元素错误: " + item)
		}
		key := keyValue[0]
		if !tools.StringSliceContain(config.AllowedFormName, key) {
			return nil, errors.New("不允许的表单的值: " + key)
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
			return nil, errors.New("错误的关键词格式: " + item)
		}
		key := keyValue[0]
		value := keyValue[1]
		parsedData[key] = value
	}
	return parsedData, nil
}

func outputResult(result *pkg.Result) {
	// 输出结果
	if outputMode == "console" {
		for _, req := range result.ReqList {
			urls = append(urls, req.URL.String())
		}
	}
	if len(outputJsonPath) != 0 {
		resBytes := getJsonSerialize(result)
		tools.WriteFile(outputJsonPath, resBytes)
	}
}

/*
*
原生被动代理推送支持
*/
func Push2Proxy(reqList []*model2.Request) {
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
				logger.Logger.Error("加入原生被动代理推送的支持失败: ", err)
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

func handleExit(t *pkg.CrawlerTask) {
	<-signalChan
	t.Pool.Tune(1)
	t.Pool.Release()
	t.Browser.Close()
	os.Exit(-1)
}

func getJsonSerialize(result *pkg.Result) []byte {
	var res Result
	var reqList []Request
	var allReqList []Request
	for _, _req := range result.ReqList {
		var req Request
		req.Method = _req.Method
		req.Url = _req.URL.String()
		req.Source = _req.Source
		req.Data = _req.PostData
		req.Headers = _req.Headers
		reqList = append(reqList, req)
	}
	for _, _req := range result.AllReqList {
		var req Request
		req.Method = _req.Method
		req.Url = _req.URL.String()
		req.Source = _req.Source
		req.Data = _req.PostData
		req.Headers = _req.Headers
		allReqList = append(allReqList, req)
	}
	res.AllReqList = allReqList
	res.ReqList = reqList
	res.AllDomainList = result.AllDomainList
	res.SubDomainList = result.SubDomainList

	resBytes, err := json.Marshal(res)
	if err != nil {
		log.Fatal("序列化结果出错！")
	}
	return resBytes
}

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
	req       *model2.Request
	pushProxy string
}

var cliFlags = []cli.Flag{
	SetChromePath(),
	SetCustomHeaders(),
	SetMaxCrawledCount(),
	SetFilterMod(),
	SetOutputMode(),
	SetOutputJSON(),
	//SetIgcognitoContext(),
	SetMaxTabCount(),
	SetFuzzPath(),
	SetFuzzPathDict(),
	SetRobotsPath(),
	SetRequestProxy(),
	SetEncodeURL(),
	SetTabRunTTL(),
	SetWaitDomContentLoadedTTL(),
	SetEventTriggerMode(),
	SetEventTriggerInterval(),
	SetBeforeExitDelay(),
	SetIgnoreUrlKeywords(),
	SetFormValues(),
	SetFormKeywordValue(),
	SetPushToProxy(),
	SetPushPoolMax(),
	SetLogLevel(),
	SetNoHeadless(),
	SetDomain(),
}

func SetDomain() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "domain",
		Aliases:     []string{"d"},
		Usage:       "write input domain",
		Destination: &domain,
	}
}

func SetChromePath() *cli.PathFlag {
	return &cli.PathFlag{
		Name:        "chromium-path",
		Aliases:     []string{"c"},
		Usage:       "`Path` of chromium executable. Such as \"/home/test/chrome-linux/chrome\"",
		Destination: &taskConfig.ChromiumPath,
		EnvVars:     []string{"CRAWLERGO_CHROMIUM_PATH"},
	}
}

func SetCustomHeaders() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "custom-headers",
		Usage:       "add additional `Headers` to each request. The input string will be called json.Unmarshal",
		Value:       fmt.Sprintf(`{"User-Agent": "%s"}`, config.DefaultUA),
		Destination: &taskConfig.ExtraHeadersString,
	}
}

func SetMaxCrawledCount() *cli.IntFlag {
	return &cli.IntFlag{
		Name:        "max-crawled-count",
		Aliases:     []string{"m"},
		Value:       config.MaxCrawlCount,
		Usage:       "the maximum `Number` of URLs visited by the crawler in this task.",
		Destination: &taskConfig.MaxCrawlCount,
	}
}

func SetFilterMod() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "filter-mode",
		Aliases:     []string{"f"},
		Value:       "smart",
		Usage:       "filtering `Mode` used for collected requests. Allowed mode:\"simple\", \"smart\" or \"strict\".",
		Destination: &taskConfig.FilterMode,
	}
}

func SetOutputMode() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "output-mode",
		Aliases:     []string{"o"},
		Value:       "console",
		Usage:       "console print or serialize output. Allowed mode:\"console\" ,\"json\" or \"none\".",
		Destination: &outputMode,
	}
}

func SetOutputJSON() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "output-json",
		Usage:       "write output to a json file.Such as result_www_test_com.json",
		Destination: &outputJsonPath,
	}
}

func SetMaxTabCount() *cli.IntFlag {
	return &cli.IntFlag{
		Name:        "max-tab-count",
		Aliases:     []string{"t"},
		Value:       8,
		Usage:       "maximum `Number` of tabs allowed.",
		Destination: &taskConfig.MaxTabsCount,
	}
}

func SetFuzzPath() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:        "fuzz-path",
		Value:       false,
		Usage:       "whether to fuzz the target with common paths.",
		Destination: &taskConfig.PathByFuzz,
	}
}

func SetFuzzPathDict() *cli.PathFlag {
	return &cli.PathFlag{
		Name:        "fuzz-path-dict",
		Usage:       "`Path` of fuzz dict. Such as \"/home/test/fuzz_path.txt\"",
		Destination: &taskConfig.FuzzDictPath,
	}
}

func SetRobotsPath() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:        "robots-path",
		Value:       true,
		Usage:       "whether to resolve paths from /robots.txt.",
		Destination: &taskConfig.PathFromRobots,
	}
}

func SetRequestProxy() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "request-proxy",
		Usage:       "all requests connect through defined proxy server.",
		Destination: &taskConfig.Proxy,
	}
}

func SetEncodeURL() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:        "encode-url",
		Value:       false,
		Usage:       "whether to encode url with detected charset.",
		Destination: &taskConfig.EncodeURLWithCharset,
	}
}

func SetTabRunTTL() *cli.DurationFlag {

	return &cli.DurationFlag{
		Name:        "tab-run-timeout",
		Value:       config.TabRunTimeout,
		Usage:       "the `Timeout` of a single tab task.",
		Destination: &taskConfig.TabRunTimeout,
	}
}

func SetWaitDomContentLoadedTTL() *cli.DurationFlag {
	return &cli.DurationFlag{
		Name:        "wait-dom-content-loaded-timeout",
		Value:       config.DomContentLoadedTimeout,
		Usage:       "the `Timeout` of waiting for a page dom ready.",
		Destination: &taskConfig.DomContentLoadedTimeout,
	}
}

func SetEventTriggerMode() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "event-trigger-mode",
		Value:       config.EventTriggerAsync,
		Usage:       "this `Value` determines how the crawler automatically triggers events.Allowed mode:\"async\" or \"sync\".",
		Destination: &taskConfig.EventTriggerMode,
	}
}

func SetEventTriggerInterval() *cli.DurationFlag {
	return &cli.DurationFlag{
		Name:        "event-trigger-interval",
		Value:       config.EventTriggerInterval,
		Usage:       "the `Interval` of triggering each event.",
		Destination: &taskConfig.EventTriggerInterval,
	}
}

func SetBeforeExitDelay() *cli.DurationFlag {
	return &cli.DurationFlag{
		Name:        "before-exit-delay",
		Value:       config.BeforeExitDelay,
		Usage:       "the `Time` of waiting before crawler exit.",
		Destination: &taskConfig.BeforeExitDelay,
	}
}

func SetIgnoreUrlKeywords() *cli.StringSliceFlag {
	return &cli.StringSliceFlag{
		Name:        "ignore-url-keywords",
		Aliases:     []string{"iuk"},
		Value:       cli.NewStringSlice(config.DefaultIgnoreKeywords...),
		Usage:       "crawlergo will not crawl these URLs matched by `Keywords`. e.g.: -iuk logout -iuk quit -iuk exit",
		DefaultText: "Default [logout quit exit]",
		Destination: ignoreKeywords,
	}
}

func SetFormValues() *cli.StringSliceFlag {
	return &cli.StringSliceFlag{
		Name:        "form-values",
		Aliases:     []string{"fv"},
		Usage:       "custom filling text for each form type. e.g.: -fv username=admin -fv password=admin123",
		Destination: customFormTypeValues,
	}
}

// 根据关键词自行选择填充文本
func SetFormKeywordValue() *cli.StringSliceFlag {
	return &cli.StringSliceFlag{
		Name:        "form-keyword-values",
		Aliases:     []string{"fkv"},
		Usage:       "custom filling text, fuzzy matched by keyword. e.g.: -fkv user=admin -fkv pass=admin123",
		Destination: customFormKeywordValues,
	}
}

func SetPushToProxy() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "push-to-proxy",
		Usage:       "every request in 'req_list' will be pushed to the proxy `Address`. Such as \"http://127.0.0.1:8080/\"",
		Destination: &pushAddress,
	}
}

func SetPushPoolMax() *cli.IntFlag {
	return &cli.IntFlag{
		Name:        "push-pool-max",
		Usage:       "maximum `Number` of concurrency when pushing results to proxy.",
		Value:       DefaultMaxPushProxyPoolMax,
		Destination: &pushProxyPoolMax,
	}
}

func SetLogLevel() *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "log-level",
		Usage:       "log print `Level`, options include debug, info, warn, error and fatal.",
		Value:       DefaultLogLevel,
		Destination: &logLevel,
	}
}

func SetNoHeadless() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:        "no-headless",
		Value:       false,
		Usage:       "no headless mode",
		Destination: &taskConfig.NoHeadless,
	}
}
func WriteArrayToFile(array []string) error {
	content := strings.Join(array, "\n")
	err := ioutil.WriteFile("result.txt", []byte(content), 0644)
	if err != nil {
		gologger.Error().Msg("结果写入TXT失败")
		return err
	}
	gologger.Info().Msg("已将结果写入result.txt文件中")
	return nil

}
