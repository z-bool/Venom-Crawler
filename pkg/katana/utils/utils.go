package utils

import (
	"github.com/ttacon/chalk"
	"log"
	"strings"

	"github.com/lukasbob/srcset"
	urlutil "github.com/projectdiscovery/utils/url"
)

// IsURL returns true if a provided string is URL
func IsURL(url string) bool {
	if value, err := urlutil.Parse(url); err == nil {
		return value.Hostname() != ""
	} else {
		log.Println(chalk.Red.Color("error: 不能解析" + url + "错误: " + err.Error()))
	}
	return false
}

// ParseSRCSetTag parses srcset tag returning found URLs
func ParseSRCSetTag(value string) []string {
	set := srcset.Parse(value)
	values := make([]string, 0, len(set))
	for _, item := range set {
		values = append(values, item.URL)
	}
	return values
}

// ParseLinkTag parses link tag values returning found urls
//
// Inspired from: https://github.com/tomnomnom/linkheader
func ParseLinkTag(value string) []string {
	urls := make([]string, 0)

	for _, chunk := range strings.Split(value, ",") {
		for _, piece := range strings.Split(chunk, ";") {
			piece = strings.Trim(piece, " ")
			if piece == "" {
				continue
			}
			if piece[0] == '<' && piece[len(piece)-1] == '>' {
				urls = append(urls, strings.Trim(piece, "<>"))
				continue
			}
		}
	}
	return urls
}

// ParseRefreshTag parses refresh tag values returning found urls
func ParseRefreshTag(value string) string {
	chunks := strings.Split(value, "url=")
	if len(chunks) < 2 {
		return ""
	}
	chunk := chunks[1]
	chunk = strings.TrimSuffix(chunk, ";")
	if chunk == "" {
		return ""
	}
	return chunk
}

// WebUserAgent returns the chrome-web user agent
func WebUserAgent() string {
	return "Mozilla/5.0 (Macintosh; Intel Mac OS X 11_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36"
}

func FlattenHeaders(headers map[string][]string) map[string]string {
	h := make(map[string]string)
	for k, v := range headers {
		h[k] = strings.Join(v, ";")
	}
	return h
}

// ReplaceAllQueryParam replaces all the query param with the given value
func ReplaceAllQueryParam(reqUrl, val string) string {
	u, err := urlutil.Parse(reqUrl)
	if err != nil {
		return reqUrl
	}
	params := u.Query()
	for k := range params {
		params.Set(k, "")
	}
	u.RawQuery = params.Encode()
	return u.String()
}
