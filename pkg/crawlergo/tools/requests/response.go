package requests

import (
	"github.com/ttacon/chalk"
	"io/ioutil"
	"log"
	"net/http"
)

// 自定义一些函数
type Response struct {
	http.Response
	// raw text Response
	Text string
}

func getTextFromResp(r *http.Response) string {
	// TODO: 编码转换
	if r.ContentLength == 0 {
		return ""
	}
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Println(chalk.Red.Color("error: 获取响应内容错误, " + err.Error()))
	}
	_ = r.Body.Close()
	return string(b)
}

func NewResponse(r *http.Response) *Response {
	return &Response{
		Response: *r,
		Text:     getTextFromResp(r),
	}
}
