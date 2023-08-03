package runner

import (
	"github.com/ttacon/chalk"
	"log"
	"strings"

	errorutil "github.com/projectdiscovery/utils/errors"
	urlutil "github.com/projectdiscovery/utils/url"
	"github.com/remeh/sizedwaitgroup"
)

// ExecuteCrawling executes the crawling main loop
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
				log.Println(chalk.Red.Color("error: 爬行该" + input + "路径出错, " + err.Error()))
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
		log.Println(chalk.Red.Color("error: 输入的" + inputURL + "不是一个路径, " + err.Error()))
		return inputURL
	}
	if parsed.Port() != "" && (parsed.Port() == "80" || parsed.Port() == "8080") {
		return urlutil.HTTP + urlutil.SchemeSeparator + inputURL
	} else {
		return urlutil.HTTPS + urlutil.SchemeSeparator + inputURL
	}
}
